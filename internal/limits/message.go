package limits

import (
	"strconv"
	"sync"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/config"
)

//nolint:gochecknoglobals // the embedded locale bundle, built once and read-only.
var bundle = sync.OnceValue(func() *i18n.Bundle { return i18n.NewEmbeddedBundle(i18n.EnUS) })

// Message renders e in loc, ending with the deployment's way to ask for more: the contact address on cloud, the administrator when self-hosted.
func Message(loc string, mode config.Mode, e *ReachedError, contact string) string {
	t := bundle().For(i18n.Locale(loc))
	args := []any{"Org", e.Name, "Max", strconv.FormatInt(e.Max, 10), "Pending", strconv.FormatInt(e.Pending, 10)}
	key := messageKey(mode, e.Key)
	body := t.Tn(key, int(e.Max), args...)
	if body == key {
		body = t.T(key, args...)
	}
	switch {
	case mode != config.ModeCloud:
		return body + " " + t.T("limits.suffix_selfhosted")
	case contact != "":
		return body + " " + t.T("limits.suffix_cloud", "Contact", contact)
	default:
		return body
	}
}

// JoinNotice is the sign-in notice for an invite a limit refused: the org's name, then Message.
func JoinNotice(loc string, mode config.Mode, e *ReachedError, contact string) string {
	return bundle().For(i18n.Locale(loc)).T("limits.notice_join_refused", "Org", e.Name, "Message", Message(loc, mode, e, contact))
}

//i18n:use limits.message.*
//i18n:use limits.notice_join_refused

func messageKey(mode config.Mode, key Key) string {
	switch key {
	case OrgMembers:
		return "limits.message.org_members"
	case OrgProjects:
		if mode == config.ModeCloud {
			return "limits.message.org_projects_cloud"
		}
		return "limits.message.org_projects_selfhosted"
	case UserOrgsCreated:
		return "limits.message.user_orgs_created"
	case UserOrgsJoined:
		return "limits.message.user_orgs_joined"
	case OrgCustomRoles:
		return "limits.message.org_custom_roles"
	default:
		return "limits.message." + appMessageKey(key)
	}
}
