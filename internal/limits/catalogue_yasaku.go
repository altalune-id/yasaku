package limits

// ProjectWallets limits a project's active wallets.
const ProjectWallets Key = "project.wallets"

func appCatalogue() []Def {
	return []Def{
		{Key: ProjectWallets, Kind: Count, Subject: SubjectProject, Cloud: 20, SelfHosted: Unlimited, Hidden: true},
	}
}

func appMessageKey(key Key) string {
	if key == ProjectWallets {
		return "project_wallets"
	}
	return string(key)
}
