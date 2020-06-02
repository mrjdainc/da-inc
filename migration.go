package main

import (
	"github.com/mrjdainc/da-inc/repository"
	"github.com/mrjdainc/da-inc/xbmc"
)

func checkRepository() bool {
	if xbmc.IsAddonInstalled("repository.dainc") {
		if !xbmc.IsAddonEnabled("repository.dainc") {
			xbmc.SetAddonEnabled("repository.dainc", true)
		}
		return true
	}

	log.Info("Creating dainc repository add-on...")
	if err := repository.MakedaincRepositoryAddon(); err != nil {
		log.Errorf("Unable to create repository add-on: %s", err)
		return false
	}

	xbmc.UpdateLocalAddons()
	for _, addon := range xbmc.GetAddons("xbmc.addon.repository", "unknown", "all", []string{"name", "version", "enabled"}).Addons {
		if addon.ID == "repository.dainc" && addon.Enabled == true {
			log.Info("Found enabled dainc repository add-on")
			return false
		}
	}
	log.Info("dainc repository not installed, installing...")
	xbmc.InstallAddon("repository.dainc")
	xbmc.SetAddonEnabled("repository.dainc", true)
	xbmc.UpdateLocalAddons()
	xbmc.UpdateAddonRepos()

	return true
}
