package presets

import "github.com/naoray/anvil/internal/config"

func resolveSiteDriver(siteDrivers []config.SiteDriver) config.SiteDriver {
	if len(siteDrivers) > 0 && siteDrivers[0] == config.SiteDriverHerd {
		return config.SiteDriverHerd
	}
	return config.SiteDriverYerd
}

func siteScaffoldSteps(siteDriver config.SiteDriver) []config.StepConfig {
	if siteDriver == config.SiteDriverHerd {
		return []config.StepConfig{{Name: "herd", Args: []string{"link", "--secure", "{{ .SiteName }}"}}}
	}
	return []config.StepConfig{
		{Name: "yerd", Args: []string{"link", "{{ .SiteName }}"}},
		{Name: "yerd", Args: []string{"secure", "{{ .SiteName }}"}},
	}
}

func siteCleanupSteps(siteDriver config.SiteDriver) []config.CleanupStep {
	if siteDriver == config.SiteDriverHerd {
		return []config.CleanupStep{{Name: "herd"}}
	}
	return []config.CleanupStep{{Name: "yerd", Args: []string{"unlink", "{{ .SiteName }}"}}}
}
