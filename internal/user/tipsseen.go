package user

import (
	"encoding/json"
	"slices"
)

const (
	tipsSeenAttempts = 3
	emptyTipsSeen    = "[]"
)

func parseTipsSeen(raw string) []string {
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return nil
	}
	return ids
}

func mergeTipsSeen(raw string, add []string) (ids []string, encoded string) {
	ids = parseTipsSeen(raw)
	if ids == nil {
		ids = []string{}
	}
	for _, id := range add {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return ids, raw
	}
	return ids, string(b)
}
