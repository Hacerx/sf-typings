import { LightningElement } from 'lwc';
import LightningAlert from 'lightning/alert';

// These resolve from official packages, with no local module stubs.
class OfficialTypesProbe extends LightningElement {}
void OfficialTypesProbe;
// @ts-expect-error Official Lightning alert messages must be strings.
void LightningAlert.open({ message: 42 });
