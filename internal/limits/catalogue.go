package limits

const (
	// UserOrgsCreated limits the orgs one person creates.
	UserOrgsCreated Key = "user.orgs_created"
	// UserOrgsJoined limits the orgs one person belongs to, the system org aside.
	UserOrgsJoined Key = "user.orgs_joined"
	// OrgMembers limits an org's members plus its pending invites.
	OrgMembers Key = "org.members"
	// OrgProjects limits an org's projects.
	OrgProjects Key = "org.projects"
	// OrgCustomRoles limits an org's custom roles.
	OrgCustomRoles Key = "org.custom_roles"
)

// Catalogue returns every limit this deployment knows, generic and app-specific.
func Catalogue() []Def {
	return append([]Def{
		{Key: UserOrgsCreated, Kind: Count, Subject: SubjectUser, Cloud: 1, SelfHosted: Unlimited},
		{Key: UserOrgsJoined, Kind: Count, Subject: SubjectUser, Cloud: 5, SelfHosted: Unlimited},
		{Key: OrgMembers, Kind: Count, Subject: SubjectOrg, Cloud: 5, SelfHosted: Unlimited},
		{Key: OrgProjects, Kind: Count, Subject: SubjectOrg, Cloud: 3, SelfHosted: Unlimited},
		{Key: OrgCustomRoles, Kind: Count, Subject: SubjectOrg, Cloud: 10, SelfHosted: Unlimited},
	}, appCatalogue()...)
}

// Lookup returns the catalogue entry for key.
func Lookup(key Key) (Def, bool) {
	for _, d := range Catalogue() {
		if d.Key == key {
			return d, true
		}
	}
	return Def{}, false
}
