// sf-typings generates declarations from selected Salesforce object describes.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hacerx/sf-auth-decrypt-go/authdecrypt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/octoberswimmer/apexfmt/parser"
)

var apiName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var aliasName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var restVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

type config struct {
	OrgAlias    string   `json:"orgAlias"`
	Objects     []string `json:"objects"`
	ApexSources []string `json:"apexSources"`
}
type field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nillable *bool  `json:"nillable"`
}
type describe struct {
	Name   string  `json:"name"`
	Fields []field `json:"fields"`
}
type envelope struct {
	Status *int            `json:"status"`
	Result json.RawMessage `json:"result"`
}
type output struct {
	name string
	data []byte
}

func decode(data []byte, target any, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if strict {
		d.DisallowUnknownFields()
	}
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func loadConfig(path string) (config, error) {
	var c config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("cannot read config")
	}
	if err := decode(b, &c, true); err != nil {
		return c, fmt.Errorf("invalid config JSON")
	}
	if len(c.Objects) == 0 && len(c.ApexSources) == 0 {
		return c, fmt.Errorf("objects or apexSources must be nonempty")
	}
	if len(c.Objects) > 0 && !aliasName.MatchString(c.OrgAlias) {
		return c, fmt.Errorf("invalid org alias")
	}
	for _, source := range c.ApexSources {
		if source == "" {
			return c, fmt.Errorf("invalid Apex source path")
		}
	}
	seen := make(map[string]bool)
	for _, name := range c.Objects {
		if !apiName.MatchString(name) {
			return c, fmt.Errorf("invalid object API name")
		}
		if seen[name] {
			return c, fmt.Errorf("duplicate object API name")
		}
		seen[name] = true
	}
	sort.Strings(c.Objects)
	return c, nil
}

type credentials struct{ token, instance string }
type credentialResolver func(context.Context, string) (credentials, error)

func localCredentials(ctx context.Context, alias string) (credentials, error) {
	client, err := authdecrypt.New()
	if err != nil {
		return credentials{}, fmt.Errorf("local org unavailable")
	}
	record, err := client.ResolveOrg(ctx, alias)
	if err != nil {
		return credentials{}, fmt.Errorf("local org unavailable")
	}
	token, _ := record["accessToken"].(string)
	instance, _ := record["instanceUrl"].(string)
	return credentials{token, instance}, nil
}

// cappedWriter bounds CLI stdout without ever including its contents in errors.
type cappedWriter struct {
	bytes.Buffer
	limit int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		return 0, fmt.Errorf("response too large")
	}
	return w.Buffer.Write(p)
}

func cliJSON(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sf", args...)
	buf := &cappedWriter{limit: 1 << 20}
	cmd.Stdout = buf
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return nil, fmt.Errorf("Salesforce CLI unavailable")
	}
	return buf.Bytes(), nil
}

func cliCredentials(ctx context.Context, alias string) (credentials, error) {
	data, err := cliJSON(ctx, "org", "auth", "show-access-token", "-o", alias, "--no-prompt", "--json")
	if err != nil {
		return credentials{}, fmt.Errorf("org token unavailable")
	}
	var response struct {
		Status *int `json:"status"`
		Result struct { AccessToken string `json:"accessToken"` } `json:"result"`
	}
	if decode(data, &response, false) != nil || response.Status == nil || *response.Status != 0 || response.Result.AccessToken == "" {
		return credentials{}, fmt.Errorf("org token unavailable")
	}
	return credentials{token: response.Result.AccessToken}, nil
}

// Display is only an instance URL source; never decode or use its accessToken.
func cliInstanceURL(ctx context.Context, alias string) (string, error) {
	data, err := cliJSON(ctx, "org", "display", "--target-org", alias, "--json")
	if err != nil {
		return "", fmt.Errorf("org instance unavailable")
	}
	var response struct {
		Status *int `json:"status"`
		Result struct { InstanceURL string `json:"instanceUrl"` } `json:"result"`
	}
	if decode(data, &response, false) != nil || response.Status == nil || *response.Status != 0 || response.Result.InstanceURL == "" {
		return "", fmt.Errorf("org instance unavailable")
	}
	return response.Result.InstanceURL, nil
}

func trustedInstance(instance string) (*url.URL, bool) {
	u, err := url.Parse(instance)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(host, ".") {
		return nil, false
	}
	trusted := false
	for _, suffix := range []string{".my.salesforce.com", ".salesforce.com", ".force.com"} {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			trusted = true
		}
	}
	if !trusted {
		return nil, false
	}
	return u, true
}

func restJSON(ctx context.Context, client *http.Client, base *url.URL, path, token string) ([]byte, error) {
	u := *base
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid REST request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("REST network request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("REST HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, fmt.Errorf("invalid REST response")
	}
	return data, nil
}

func objectOutputs(ctx context.Context, alias string, names []string, snapshots bool, resolve credentialResolver, httpClient *http.Client) ([]output, error) {
	return objectOutputsWithFallback(ctx, alias, names, snapshots, resolve, cliCredentials, cliInstanceURL, httpClient)
}

func objectOutputsWithFallback(ctx context.Context, alias string, names []string, snapshots bool, resolve, token credentialResolver, instanceURL func(context.Context, string) (string, error), httpClient *http.Client) ([]output, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	creds, err := resolve(ctx, alias)
	if err != nil {
		// An entirely unavailable local org may use display for the URL only.
		creds = credentials{}
		creds.instance, err = instanceURL(ctx, alias)
		if err != nil {
			return nil, fmt.Errorf("org instance unavailable")
		}
	} else if creds.instance == "" {
		return nil, fmt.Errorf("local org instance unavailable")
	}
	if _, ok := trustedInstance(creds.instance); !ok {
		return nil, fmt.Errorf("untrusted org instance")
	}
	if creds.token == "" {
		instance := creds.instance
		creds, err = token(ctx, alias)
		if err != nil || creds.token == "" {
			return nil, fmt.Errorf("org token unavailable")
		}
		creds.instance = instance // Ignore any instance returned by a token resolver.
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return restOutputs(ctx, names, snapshots, creds, &client)
}

func restOutputs(ctx context.Context, names []string, snapshots bool, creds credentials, client *http.Client) ([]output, error) {
	base, ok := trustedInstance(creds.instance)
	if !ok {
		return nil, fmt.Errorf("untrusted org instance")
	}
	versionsRaw, err := restJSON(ctx, client, base, "/services/data/", creds.token)
	if err != nil {
		return nil, fmt.Errorf("REST version lookup: %w", err)
	}
	var versions []struct {
		Version string `json:"version"`
	}
	if decode(versionsRaw, &versions, false) != nil {
		return nil, fmt.Errorf("invalid REST versions")
	}
	version, major, minor := "", "", ""
	for _, item := range versions {
		if !restVersion.MatchString(item.Version) {
			continue
		}
		parts := strings.SplitN(item.Version, ".", 2)
		for i := range parts {
			parts[i] = strings.TrimLeft(parts[i], "0")
			if parts[i] == "" {
				parts[i] = "0"
			}
		}
		// Compare decimal integers without overflow or floating-point rounding.
		greater := version == "" || len(parts[0]) > len(major) || (len(parts[0]) == len(major) && parts[0] > major) ||
			(parts[0] == major && (len(parts[1]) > len(minor) || (len(parts[1]) == len(minor) && parts[1] > minor)))
		if greater {
			version, major, minor = item.Version, parts[0], parts[1]
		}
	}
	if version == "" {
		return nil, fmt.Errorf("invalid REST versions")
	}
	var files []output
	for _, name := range names {
		raw, err := restJSON(ctx, client, base, "/services/data/v"+version+"/sobjects/"+name+"/describe", creds.token)
		if err != nil {
			return nil, fmt.Errorf("REST Describe for %s: %w", name, err)
		}
		wrapped, err := json.Marshal(struct {
			Status int             `json:"status"`
			Result json.RawMessage `json:"result"`
		}{0, raw})
		if err != nil {
			return nil, fmt.Errorf("invalid REST response")
		}
		generated, err := generate(name, wrapped, snapshots)
		if err != nil {
			return nil, err
		}
		files = append(files, generated...)
	}
	return files, nil
}

func tsType(kind string) string {
	switch kind {
	case "string", "textarea", "picklist", "multipicklist", "id", "reference", "phone", "email", "url", "encryptedstring", "combobox", "base64":
		return "string"
	case "boolean":
		return "boolean"
	case "int", "double", "currency", "percent", "long":
		return "number"
	case "date", "datetime", "time":
		return "string"
	default:
		return "unknown"
	}
}

func generate(name string, raw []byte, snapshots bool) ([]output, error) {
	invalid := fmt.Errorf("invalid describe for %s", name)
	var env envelope
	if decode(raw, &env, false) != nil || env.Status == nil || *env.Status != 0 || len(env.Result) == 0 {
		return nil, invalid
	}
	var desc describe
	if decode(env.Result, &desc, false) != nil || desc.Name != name || desc.Fields == nil {
		return nil, invalid
	}
	seen := make(map[string]bool)
	for _, f := range desc.Fields {
		if !apiName.MatchString(f.Name) || f.Type == "" || f.Nillable == nil || seen[f.Name] {
			return nil, invalid
		}
		seen[f.Name] = true
	}
	sort.Slice(desc.Fields, func(i, j int) bool { return desc.Fields[i].Name < desc.Fields[j].Name })
	var text strings.Builder
	fmt.Fprintln(&text, "// Generated by sf-typings. Do not edit.")
	fmt.Fprintf(&text, "export interface %s {\n", name)
	for _, f := range desc.Fields {
		nullable := ""
		if *f.Nillable {
			nullable = " | null"
		}
		fmt.Fprintf(&text, "  %s?: %s%s;\n", f.Name, tsType(f.Type), nullable)
	}
	fmt.Fprintln(&text, "}")
	files := []output{{name + ".d.ts", []byte(text.String())}}
	if snapshots {
		var value any
		if decode(env.Result, &value, false) != nil {
			return nil, invalid
		}
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, invalid
		}
		files = append(files, output{name + ".describe.json", append(b, '\n')})
	}
	return files, nil
}

type syntaxErrors struct {
	*antlr.DefaultErrorListener
	failed bool
}

func (s *syntaxErrors) SyntaxError(antlr.Recognizer, interface{}, int, int, string, antlr.RecognitionException) {
	s.failed = true
}

func apexType(t parser.ITypeRefContext) string {
	if t == nil || len(t.AllTypeName()) != 1 || (t.ArraySubscripts() != nil && t.ArraySubscripts().GetText() != "") {
		return "unknown"
	}
	n := t.TypeName(0)
	if n.LIST() != nil {
		args := n.TypeArguments()
		if args != nil && args.TypeList() != nil && len(args.TypeList().AllTypeRef()) == 1 {
			return apexType(args.TypeList().TypeRef(0)) + "[]"
		}
		return "unknown"
	}
	if n.TypeArguments() != nil {
		return "unknown"
	}
	switch strings.ToLower(n.GetText()) {
	case "void":
		return "void"
	case "string", "id":
		return "string"
	case "integer", "long", "double", "decimal":
		return "number"
	case "boolean":
		return "boolean"
	case "date", "datetime", "time":
		return "string"
	default:
		return "unknown"
	}
}

func apexDeclaration(path string) (output, error) {
	invalid := fmt.Errorf("invalid Apex source")
	data, err := os.ReadFile(path)
	if err != nil {
		return output{}, fmt.Errorf("cannot read Apex source")
	}
	listener := &syntaxErrors{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	lexer := parser.NewApexLexer(antlr.NewInputStream(string(data)))
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(listener)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := parser.NewApexParser(stream)
	p.RemoveErrorListeners()
	p.AddErrorListener(listener)
	unit := p.CompilationUnit()
	if listener.failed || unit.TypeDeclaration() == nil || unit.TypeDeclaration().ClassDeclaration() == nil {
		return output{}, invalid
	}
	class := unit.TypeDeclaration().ClassDeclaration()
	name := class.Id().GetText()
	if !apiName.MatchString(name) || !strings.EqualFold(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), name) {
		return output{}, invalid
	}
	var text strings.Builder
	fmt.Fprintln(&text, "// Generated by sf-typings. Do not edit.")
	seen := make(map[string]bool)
	for _, decl := range class.ClassBody().AllClassBodyDeclaration() {
		member := decl.MemberDeclaration()
		if member == nil || member.MethodDeclaration() == nil {
			continue
		}
		enabled, accessible, static := false, false, false
		for _, mod := range decl.AllModifier() {
			if a := mod.Annotation(); a != nil && strings.EqualFold(a.QualifiedName().GetText(), "AuraEnabled") {
				enabled = true
			}
			accessible = accessible || mod.PUBLIC() != nil || mod.GLOBAL() != nil
			static = static || mod.STATIC() != nil
		}
		if !enabled || !accessible || !static {
			continue
		}
		method := member.MethodDeclaration()
		methodName := method.MethodId().GetText()
		if !apiName.MatchString(methodName) || seen[strings.ToLower(methodName)] {
			return output{}, invalid
		}
		seen[strings.ToLower(methodName)] = true
		var params []string
		if list := method.FormalParameters().FormalParameterList(); list != nil {
			for _, param := range list.AllFormalParameter() {
				paramName := param.Id().GetText()
				if !apiName.MatchString(paramName) {
					return output{}, invalid
				}
				params = append(params, fmt.Sprintf("%s: %s", paramName, apexType(param.TypeRef())))
			}
		}
		ret := "void"
		if method.VOID() == nil {
			ret = apexType(method.TypeRef())
		}
		fmt.Fprintf(&text, "declare module '@salesforce/apex/%s.%s' {\n", name, methodName)
		if len(params) == 0 {
			fmt.Fprintf(&text, "  export default function %s(): Promise<%s>;\n", methodName, ret)
		} else {
			fmt.Fprintf(&text, "  export default function %s(params: { %s }): Promise<%s>;\n", methodName, strings.Join(params, "; "), ret)
		}
		fmt.Fprintln(&text, "}")
	}
	return output{name + ".d.ts", []byte(text.String())}, nil
}

func apexOutputs(sources []string) ([]output, error) {
	var paths []string
	for _, source := range sources {
		info, err := os.Lstat(source)
		if err != nil {
			return nil, fmt.Errorf("cannot read Apex source")
		}
		if info.IsDir() {
			err = filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".cls") {
					paths = append(paths, path)
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("cannot read Apex source")
			}
		} else if info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(source), ".cls") {
			paths = append(paths, source)
		} else {
			return nil, fmt.Errorf("invalid Apex source path")
		}
	}
	sort.Strings(paths)
	var result []output
	seen := make(map[string]bool)
	for _, path := range paths {
		f, err := apexDeclaration(path)
		if err != nil {
			return nil, err
		}
		if seen[strings.ToLower(f.name)] {
			return nil, fmt.Errorf("duplicate Apex class")
		}
		seen[strings.ToLower(f.name)] = true
		result = append(result, f)
	}
	return result, nil
}

func save(dir string, files []output) error {
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		return fmt.Errorf("unsafe output directory")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect output directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create output directory")
	}
	// Reject non-regular targets before touching any generated file.
	for _, f := range files {
		info, err := os.Lstat(filepath.Join(dir, f.name))
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe output target")
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect output target")
		}
	}
	type staged struct{ target, temp string }
	var pending []staged
	defer func() {
		for _, item := range pending {
			os.Remove(item.temp)
		}
	}()
	for _, f := range files {
		tmp, err := os.CreateTemp(dir, ".sf-typings-*")
		if err != nil {
			return fmt.Errorf("cannot stage output")
		}
		pending = append(pending, staged{filepath.Join(dir, f.name), tmp.Name()})
		if _, err = tmp.Write(f.data); err != nil {
			tmp.Close()
			return fmt.Errorf("cannot stage output")
		}
		if err = tmp.Close(); err != nil {
			return fmt.Errorf("cannot stage output")
		}
	}
	for _, item := range pending {
		if err := os.Rename(item.temp, item.target); err != nil {
			return fmt.Errorf("cannot publish output")
		}
	}
	return nil
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithDeps(args, stdout, stderr, localCredentials, nil)
}

func runWithDeps(args []string, stdout, stderr io.Writer, resolve credentialResolver, client *http.Client) int {
	flags := flag.NewFlagSet("sf-typings", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cfg := flags.String("config", "", "JSON configuration path")
	out := flags.String("out", "", "output directory")
	snapshots := flags.Bool("snapshots", false, "also save complete per-object Describe JSON")
	help := flags.Bool("help", false, "show usage")
	usage := "Usage: sf-typings -config config.json -out generated [-snapshots]\n"
	if err := flags.Parse(args); err != nil {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
		return 1
	}
	if *help {
		fmt.Fprint(stdout, usage+"Config: {\"orgAlias\":\"dev\",\"objects\":[\"Account\"],\"apexSources\":[\"classes\"]}\nObjects use local auth and direct REST Describe. When the local token is absent, sf org auth show-access-token -o <alias> --no-prompt --json supplies only the token; when the local org is unavailable, sf org display supplies only the instance URL. No HTTP 401 retry. Apex-only runs use local .cls files without an org alias. Never retrieves records.\n")
		return 0
	}
	if *cfg == "" || *out == "" || flags.NArg() != 0 {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
		return 1
	}
	c, err := loadConfig(*cfg)
	apexCount := 0
	if err == nil {
		var files []output
		if len(c.Objects) > 0 {
			files, err = objectOutputs(context.Background(), c.OrgAlias, c.Objects, *snapshots, resolve, client)
		}
		if err == nil {
			var apexFiles []output
			apexFiles, err = apexOutputs(c.ApexSources)
			apexCount = len(apexFiles)
			files = append(files, apexFiles...)
		}
		if err == nil {
			seen := make(map[string]bool)
			for _, f := range files {
				if seen[strings.ToLower(f.name)] {
					err = fmt.Errorf("duplicate output name")
					break
				}
				seen[strings.ToLower(f.name)] = true
			}
		}
		if err == nil {
			err = save(*out, files)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "sf-typings: %s\n", err)
		return 1
	}
	if len(c.ApexSources) == 0 {
		fmt.Fprintf(stdout, "Generated %d object(s).\n", len(c.Objects))
	} else {
		fmt.Fprintf(stdout, "Generated %d object(s), %d Apex class(es).\n", len(c.Objects), apexCount)
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
