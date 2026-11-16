package templates

func flashClass(kind string) string {
	switch kind {
	case "ok":
		return "alert alert-soft alert-success"
	case "warn":
		return "alert alert-soft alert-warning"
	case "err":
		return "alert alert-soft alert-error"
	}
	return "alert alert-soft"
}
