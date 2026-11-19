package templates

//i18n:use opensheet.state.off
//i18n:use opensheet.state.paused
//i18n:use opensheet.state.off_manual
//i18n:use opensheet.state.failing
//i18n:use opensheet.state.given_up
//i18n:use opensheet.state.first
//i18n:use opensheet.state.pending
//i18n:use opensheet.state.ok

// OpensheetState is the Google Sheets link state the page headlines; its value names the opensheet.state.<state> key.
type OpensheetState string

// The eight link states, in precedence order.
const (
	OpensheetOff       OpensheetState = "off"
	OpensheetPaused    OpensheetState = "paused"
	OpensheetOffManual OpensheetState = "off_manual"
	OpensheetFailing   OpensheetState = "failing"
	OpensheetGivenUp   OpensheetState = "given_up"
	OpensheetFirst     OpensheetState = "first"
	OpensheetPending   OpensheetState = "pending"
	OpensheetOK        OpensheetState = "ok"
)

func (v OpensheetView) headlineKey() string {
	return "opensheet.state." + string(v.State)
}
