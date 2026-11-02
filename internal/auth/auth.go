package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/Hacerx/sf-typings/internal/jsonutil"

	"github.com/hacerx/sf-auth-decrypt-go/authdecrypt"
)

// Credentials holds a bearer and instance URL in memory; never log it.
type Credentials struct{ Token, Instance string }

// Resolver permits the CLI to inject credential sources without a client framework.
type Resolver func(context.Context, string) (Credentials, error)

// Local resolves a cached org using authdecrypt, redacting any failure.
func Local(ctx context.Context, alias string) (Credentials, error) {
	client, err := authdecrypt.New()
	if err != nil {
		return Credentials{}, fmt.Errorf("local org unavailable")
	}
	record, err := client.ResolveOrg(ctx, alias)
	if err != nil {
		return Credentials{}, fmt.Errorf("local org unavailable")
	}
	token, _ := record["accessToken"].(string)
	instance, _ := record["instanceUrl"].(string)
	if instance == "" {
		return Credentials{}, fmt.Errorf("local org unavailable")
	}
	return Credentials{token, instance}, nil
}

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

// Token obtains only the access token from sf; its response cannot change the URL.
func Token(ctx context.Context, alias string) (Credentials, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sf", "org", "auth", "show-access-token", "-o", alias, "--no-prompt", "--json")
	var buf cappedWriter
	buf.limit = 1 << 20
	cmd.Stdout = &buf
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return Credentials{}, fmt.Errorf("org token unavailable")
	}
	var result struct {
		Status *int `json:"status"`
		Result struct {
			AccessToken string `json:"accessToken"`
		} `json:"result"`
	}
	if jsonutil.Decode(buf.Bytes(), &result, false) != nil || result.Status == nil || *result.Status != 0 || result.Result.AccessToken == "" {
		return Credentials{}, fmt.Errorf("org token unavailable")
	}
	return Credentials{Token: result.Result.AccessToken}, nil
}

// InstanceURL reads only the instance URL. Even if sf org display includes an
// access token, that value is never decoded or used for authentication.
func InstanceURL(ctx context.Context, alias string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sf", "org", "display", "--target-org", alias, "--json")
	var buf cappedWriter
	buf.limit = 1 << 20
	cmd.Stdout = &buf
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", fmt.Errorf("org instance unavailable")
	}
	var result struct {
		Status *int `json:"status"`
		Result struct {
			InstanceURL string `json:"instanceUrl"`
		} `json:"result"`
	}
	if jsonutil.Decode(buf.Bytes(), &result, false) != nil || result.Status == nil || *result.Status != 0 || result.Result.InstanceURL == "" {
		return "", fmt.Errorf("org instance unavailable")
	}
	return result.Result.InstanceURL, nil
}
