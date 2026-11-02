public class OfflineProof {
    @AuraEnabled
    public static Envelope echo(String greeting) {
        return null;
    }
    public class Envelope {
        @AuraEnabled public String greeting;
        @AuraEnabled public Map<String, List<Integer>> counts;
    }
}
