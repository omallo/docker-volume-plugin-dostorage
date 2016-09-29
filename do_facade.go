package main

import (
	"fmt"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/digitalocean/go-metadata"
	"github.com/digitalocean/godo"
)

const (
	StorageActionRetryCount             = 3
	StorageActionRetryInterval          = 1000 * time.Millisecond
	StorageActionCompletionPollCount    = 60
	StorageActionCompletionPollInterval = 500 * time.Millisecond
	MaxResultsPerPage                   = 200
)

type DoFacade struct {
	metadataClient *metadata.Client
	apiClient      *godo.Client
}

func NewDoFacade(metadataClient *metadata.Client, apiClient *godo.Client) *DoFacade {
	return &DoFacade{
		metadataClient: metadataClient,
		apiClient:      apiClient,
	}
}

func (s DoFacade) GetLocalRegion() (string, error) {
	return s.metadataClient.Region()
}

func (s DoFacade) GetLocalDropletID() (int, error) {
	return s.metadataClient.DropletID()
}

func (s DoFacade) GetVolume(volumeID string) (*godo.Volume, error) {
	doVolume, _, err := s.apiClient.Storage.GetVolume(volumeID)
	return doVolume, err
}

func (s DoFacade) GetVolumeByRegionAndName(region string, name string) *godo.Volume {
	doVolumes, _, err := s.apiClient.Storage.ListVolumes(&godo.ListOptions{Page: 1, PerPage: MaxResultsPerPage})

	if err != nil {
		logrus.Errorf("failed to get the volume by region and name: %v", err)
		return nil
	}

	for i := range doVolumes {
		if doVolumes[i].Region.Slug == region && doVolumes[i].Name == name {
			return &doVolumes[i]
		}
	}
	return nil
}

func (s DoFacade) IsVolumeAttachedToDroplet(volumeID string, dropletID int) bool {
	doVolume, _, err := s.apiClient.Storage.GetVolume(volumeID)

	if err != nil {
		logrus.Errorf("failed to get the volume: %v", err)
		return false
	}

	for _, attachedDropletID := range doVolume.DropletIDs {
		if attachedDropletID == dropletID {
			return true
		}
	}
	return false
}

func (s DoFacade) DetachVolumeFromAllDroplets(volumeID string) error {
	logrus.Infof("detaching the volume '%v' from all droplets", volumeID)

	attachedDropletIDs := s.getAttachedDroplets(volumeID)
	for _, attachedDropletID := range attachedDropletIDs {
		derr := s.DetachVolumeFromDroplet(volumeID, attachedDropletID)
		if derr != nil {
			return derr
		}
	}
	return nil
}

func (s DoFacade) DetachVolumeFromDroplet(volumeID string, dropletID int) error {
	logrus.Infof("detaching the volume from the droplet %v", dropletID)

	var lastErr error

	for i := 1; i <= StorageActionRetryCount; i++ {
		action, _, derr := s.apiClient.StorageActions.Detach(volumeID, dropletID)
		if derr != nil {
			logrus.Errorf("failed to detach the volume: %v", lastErr)
			time.Sleep(StorageActionRetryInterval)
			lastErr = derr
		} else {
			lastErr = s.waitForVolumeActionToComplete(volumeID, action.ID)
			break
		}
	}

	return lastErr
}

func (s DoFacade) AttachVolumeToDroplet(volumeID string, dropletID int) error {
	logrus.Infof("detaching the volume '%v' from the droplet %v", dropletID)

	var lastErr error

	for i := 1; i <= StorageActionRetryCount; i++ {
		action, _, aerr := s.apiClient.StorageActions.Attach(volumeID, dropletID)
		if aerr != nil {
			logrus.Errorf("failed to attach the volume: %v", aerr)
			time.Sleep(StorageActionRetryInterval)
			lastErr = aerr
		} else {
			lastErr = s.waitForVolumeActionToComplete(volumeID, action.ID)
			break
		}
	}

	return lastErr
}

func (s DoFacade) getAttachedDroplets(volumeID string) []int {
	doVolume, _, err := s.apiClient.Storage.GetVolume(volumeID)
	if err != nil {
		logrus.Errorf("Error getting the volume: %v", err.Error())
		return []int{}
	}
	return doVolume.DropletIDs
}

func (s DoFacade) waitForVolumeActionToComplete(volumeID string, actionID int) error {
	logrus.Infof("waiting for the storage action %v to complete", actionID)

	lastStatus := "n/a"

	for i := 1; i <= StorageActionCompletionPollCount; i++ {
		action, _, aerr := s.apiClient.StorageActions.Get(volumeID, actionID)
		if aerr == nil {
			lastStatus = action.Status
			if action.Status == "completed" || action.Status == "errored" {
				break
			}
		} else {
			logrus.Errorf("failed to query the storage action: %v", aerr)
		}
		time.Sleep(StorageActionCompletionPollInterval)
	}

	if lastStatus == "completed" {
		logrus.Info("the action completed")
		return nil
	}

	logrus.Errorf("the action did not complete but ended with status '%v'", lastStatus)
	return fmt.Errorf("the action did not complete but ended with status '%v'", lastStatus)
}
