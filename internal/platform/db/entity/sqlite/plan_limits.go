package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// PlanLimits is the jet binding for the plan_limits table.
type PlanLimits struct {
	sqlite.Table

	Plan      sqlite.ColumnString
	Key       sqlite.ColumnString
	Value     sqlite.ColumnInteger
	UpdatedAt sqlite.ColumnString
	UpdatedBy sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewPlanLimits builds the plan_limits binding.
func NewPlanLimits(tablePrefix string) *PlanLimits {
	var (
		plan      = sqlite.StringColumn("plan")
		key       = sqlite.StringColumn("key")
		value     = sqlite.IntegerColumn("value")
		updatedAt = sqlite.StringColumn("updated_at")
		updatedBy = sqlite.StringColumn("updated_by")
		all       = sqlite.ColumnList{plan, key, value, updatedAt, updatedBy}
	)
	return &PlanLimits{
		Table:      sqlite.NewTable("", tablePrefix+"plan_limits", "plan_limits", all...),
		Plan:       plan,
		Key:        key,
		Value:      value,
		UpdatedAt:  updatedAt,
		UpdatedBy:  updatedBy,
		AllColumns: all,
	}
}

// LimitOverrides is the jet binding for the limit_overrides table.
type LimitOverrides struct {
	sqlite.Table

	SubjectKind sqlite.ColumnString
	SubjectID   sqlite.ColumnString
	Key         sqlite.ColumnString
	Value       sqlite.ColumnInteger
	UpdatedAt   sqlite.ColumnString
	UpdatedBy   sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewLimitOverrides builds the limit_overrides binding.
func NewLimitOverrides(tablePrefix string) *LimitOverrides {
	var (
		subjectKind = sqlite.StringColumn("subject_kind")
		subjectID   = sqlite.StringColumn("subject_id")
		key         = sqlite.StringColumn("key")
		value       = sqlite.IntegerColumn("value")
		updatedAt   = sqlite.StringColumn("updated_at")
		updatedBy   = sqlite.StringColumn("updated_by")
		all         = sqlite.ColumnList{subjectKind, subjectID, key, value, updatedAt, updatedBy}
	)
	return &LimitOverrides{
		Table:       sqlite.NewTable("", tablePrefix+"limit_overrides", "limit_overrides", all...),
		SubjectKind: subjectKind,
		SubjectID:   subjectID,
		Key:         key,
		Value:       value,
		UpdatedAt:   updatedAt,
		UpdatedBy:   updatedBy,
		AllColumns:  all,
	}
}
