@IsTest
private class SfTypingsExampleControllerTest {
    @IsTest
    static void returnsSyntheticAccount() {
        SfTypingsExampleResult result = SfTypingsExampleController.getExample('Hello');
        System.assertEquals('Hello Account', result.account.Name);
        System.assertEquals(3, result.account.NumberOfEmployees);
        System.assertEquals(null, result.account.Id);
        System.assertEquals(new Map<String, Integer>{ 'employees' => 3 }, result.counts);
        System.assertEquals(new List<String>{ 'synthetic', 'no records read' }, result.labels);
    }
}
