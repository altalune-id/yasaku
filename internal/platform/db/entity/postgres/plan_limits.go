package postgres

import "github.com/go-jet/jet/v2/postgres"

// PlanLimits is the jet binding for the plan_limits table.
type PlanLimits struct {
	postgres.Table

	Plan      postgres.ColumnString
	Key       postgres.ColumnString
	Value     postgres.ColumnInteger
	UpdatedAt postgres.ColumnTimestampz
	UpdatedBy postgres.ColumnString

	AllColumns postgres.ColumnList
}

// NewPlanLimits builds the plan_limits binding.
func NewPlanLimits(schema, tablePrefix string) *PlanLimits {
	if schema == "" {
		schema = "public"
	}
	var (
		plan      = postgres.StringColumn("plan")
		key       = postgres.StringColumn("key")
		value     = postgres.IntegerColumn("value")
		updatedAt = postgres.TimestampzColumn("updated_at")
		updatedBy = postgres.StringColumn("updated_by")
		all       = postgres.ColumnList{plan, key, value, updatedAt, updatedBy}
	)
	return &PlanLimits{
		Table:      postgres.NewTable(schema, tablePrefix+"plan_limits", "plan_limits", all...),
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
	postgres.Table

	SubjectKind postgres.ColumnString
	SubjectID   postgres.ColumnString
	Key         postgres.ColumnString
	Value       postgres.ColumnInteger
	UpdatedAt   postgres.ColumnTimestampz
	UpdatedBy   postgres.ColumnString

	AllColumns postgres.ColumnList
}

// NewLimitOverrides builds the limit_overrides binding.
func NewLimitOverrides(schema, tablePrefix string) *LimitOverrides {
	if schema == "" {
		schema = "public"
	}
	var (
		subjectKind = postgres.StringColumn("subject_kind")
		subjectID   = postgres.StringColumn("subject_id")
		key         = postgres.StringColumn("key")
		value       = postgres.IntegerColumn("value")
		updatedAt   = postgres.TimestampzColumn("updated_at")
		updatedBy   = postgres.StringColumn("updated_by")
		all         = postgres.ColumnList{subjectKind, subjectID, key, value, updatedAt, updatedBy}
	)
	return &LimitOverrides{
		Table:       postgres.NewTable(schema, tablePrefix+"limit_overrides", "limit_overrides", all...),
		SubjectKind: subjectKind,
		SubjectID:   subjectID,
		Key:         key,
		Value:       value,
		UpdatedAt:   updatedAt,
		UpdatedBy:   updatedBy,
		AllColumns:  all,
	}
}
