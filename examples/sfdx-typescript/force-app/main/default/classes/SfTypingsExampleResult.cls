public class SfTypingsExampleResult {
    @AuraEnabled public Account account { get; set; }
    @AuraEnabled public Map<String, Integer> counts { get; set; }
    @AuraEnabled public List<String> labels { get; set; }
}
