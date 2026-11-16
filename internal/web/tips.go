package web

import "slices"

// Tip is one registered tip: its id, the flow it belongs to, its translation key and the data-tour target it anchors to.
type Tip struct {
	ID      string
	Flow    string
	TextKey string
	Target  string
}

// MaxTipsPerFlow caps how many tips one flow may register.
const MaxTipsPerFlow = 3

// LookupTip returns the registered tip with id.
func LookupTip(id string) (Tip, bool) {
	i := slices.IndexFunc(Tips(), func(t Tip) bool { return t.ID == id })
	if i < 0 {
		return Tip{}, false
	}
	return Tips()[i], true
}

// FlowIDs returns the ids of every tip in t's flow.
func (t Tip) FlowIDs() []string {
	var ids []string
	for _, o := range Tips() {
		if o.Flow == t.Flow {
			ids = append(ids, o.ID)
		}
	}
	return ids
}

// TipSeen reports whether the signed-in principal has dismissed the tip with id.
func (d LayoutData) TipSeen(id string) bool {
	return d.Principal != nil && slices.Contains(d.Principal.TipsSeen, id)
}
