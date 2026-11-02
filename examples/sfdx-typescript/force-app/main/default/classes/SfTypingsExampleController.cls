public with sharing class SfTypingsExampleController {
    @AuraEnabled(cacheable=true)
    public static SfTypingsExampleResult getExample(String greeting) {
        SfTypingsExampleResult result = new SfTypingsExampleResult();
        result.account = new Account(Name = greeting + ' Account', NumberOfEmployees = 3);
        result.counts = new Map<String, Integer>{ 'employees' => 3 };
        result.labels = new List<String>{ 'synthetic', 'no records read' };
        return result;
    }
}
