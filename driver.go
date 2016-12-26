package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const (
	DriverName       = "dostorage"
	MetadataDirMode  = 0700
	MetadataFileMode = 0600
	MountDirMode     = os.ModeDir
)

type Driver struct {
	region           string
	dropletID        int
	volumes          map[string]*VolumeState
	baseMetadataPath string
	baseMountPath    string
	doFacade         *DoFacade
	mountUtil        *MountUtil
	m                *sync.Mutex
}

type VolumeState struct {
	doVolumeID     string
	mountpoint     string
	referenceCount int
}

func NewDriver(doFacade *DoFacade, mountUtil *MountUtil, baseMetadataPath string, baseMountPath string) (*Driver, error) {
	logrus.Info("creating a new driver instance")

	region, rerr := doFacade.GetLocalRegion()
	if rerr != nil {
		return nil, rerr
	}

	dropletID, derr := doFacade.GetLocalDropletID()
	if derr != nil {
		return nil, derr
	}

	merr := os.MkdirAll(baseMetadataPath, MetadataDirMode)
	if merr != nil {
		return nil, merr
	}

	terr := os.MkdirAll(baseMountPath, MountDirMode)
	if terr != nil {
		return nil, terr
	}

	logrus.Infof("droplet metadata: region='%v', dropletID=%v", region, dropletID)

	driver := &Driver{
		region:           region,
		dropletID:        dropletID,
		volumes:          make(map[string]*VolumeState),
		baseMetadataPath: baseMetadataPath,
		baseMountPath:    baseMountPath,
		doFacade:         doFacade,
		mountUtil:        mountUtil,
		m:                &sync.Mutex{},
	}

	ierr := driver.initVolumesFromMetadata()
	if ierr != nil {
		return nil, ierr
	}

	return driver, nil
}

func (d Driver) Create(r volume.Request) volume.Response {
	logrus.Infof("[Create]: %+v", r)

	d.m.Lock()
	defer d.m.Unlock()

	volumeState, ierr := d.initVolume(r.Name)
	if ierr != nil {
		return volume.Response{Err: ierr.Error()}
	}

	metadataFilePath := filepath.Join(d.baseMetadataPath, r.Name)

	metadataFile, ferr := os.Create(metadataFilePath)
	if ferr != nil {
		logrus.Errorf("failed to create metadata file '%v' for volume '%v'", metadataFilePath, r.Name)
		return volume.Response{Err: ferr.Error()}
	}

	cerr := metadataFile.Chmod(MetadataFileMode)
	if cerr != nil {
		os.Remove(metadataFilePath)
		logrus.Errorf("failed to change the mode for the metadata file '%v' for volume '%v'", metadataFilePath, r.Name)
		return volume.Response{Err: cerr.Error()}
	}

	d.volumes[r.Name] = volumeState

	return volume.Response{}
}

func (d Driver) List(r volume.Request) volume.Response {
	logrus.Infof("[List]: %+v", r)

	volumes := []*volume.Volume{}

	for name, state := range d.volumes {
		volumes = append(volumes, &volume.Volume{
			Name:       name,
			Mountpoint: state.mountpoint,
		})
	}

	return volume.Response{Volumes: volumes}
}

func (d Driver) Get(r volume.Request) volume.Response {
	logrus.Infof("[Get]: %+v", r)

	if state, ok := d.volumes[r.Name]; ok {
		status := make(map[string]interface{})
		doVolume, verr := d.doFacade.GetVolume(state.doVolumeID)
		if verr == nil {
			status["VolumeID"] = state.doVolumeID
			status["ReferenceCount"] = state.referenceCount
			status["AttachedDropletIDs"] = doVolume.DropletIDs
		} else {
			logrus.Errorf("failed to get the volume with ID '%v': %v", state.doVolumeID, verr)
			status["Err"] = fmt.Sprintf("failed to get the volume with ID '%v': %v", state.doVolumeID, verr)
		}

		return volume.Response{
			Volume: &volume.Volume{
				Name:       r.Name,
				Mountpoint: state.mountpoint,
				Status:     status,
			},
		}
	}

	logrus.Infof("volume named '%v' not found", r.Name)
	return volume.Response{Err: fmt.Sprintf("volume named '%v' not found", r.Name)}
}

func (d Driver) Remove(r volume.Request) volume.Response {
	logrus.Infof("[Remove]: %+v", r)

	d.m.Lock()
	defer d.m.Unlock()

	if _, ok := d.volumes[r.Name]; ok {
		metadataFilePath := filepath.Join(d.baseMetadataPath, r.Name)

		rerr := os.Remove(metadataFilePath)
		if rerr != nil {
			logrus.Errorf("failed to delete metadata file '%v' for volume '%v", metadataFilePath, r.Name)
			return volume.Response{Err: fmt.Sprintf("failed to delete metadata file '%v' for volume '%v", metadataFilePath, r.Name)}
		}

		delete(d.volumes, r.Name)

		return volume.Response{}
	}

	logrus.Errorf("volume named '%v' not found", r.Name)
	return volume.Response{Err: fmt.Sprintf("volume named '%v' not found", r.Name)}
}

func (d Driver) Path(r volume.Request) volume.Response {
	logrus.Infof("[Path]: %+v", r)

	if state, ok := d.volumes[r.Name]; ok {
		return volume.Response{
			Mountpoint: state.mountpoint,
		}
	}

	logrus.Errorf("volume named '%v' not found", r.Name)
	return volume.Response{Err: fmt.Sprintf("volume named '%v' not found", r.Name)}
}

func (d Driver) Mount(r volume.MountRequest) volume.Response {
	logrus.Infof("[Mount]: %+v", r)

	d.m.Lock()
	defer d.m.Unlock()

	if state, ok := d.volumes[r.Name]; ok {
		state.referenceCount++

		if state.referenceCount == 1 {
			logrus.Info("mounting the volume upon detecting the first reference")

			if !d.doFacade.IsVolumeAttachedToDroplet(state.doVolumeID, d.dropletID) {
				logrus.Info("attaching the volume to this droplet")

				d.doFacade.DetachVolumeFromAllDroplets(state.doVolumeID)
				aerr := d.doFacade.AttachVolumeToDroplet(state.doVolumeID, d.dropletID)
				if aerr != nil {
					logrus.Errorf("failed to attach the volume to this droplet: %v", aerr)
					return volume.Response{Err: fmt.Sprintf("failed to attach the volume to this droplet: %v", aerr)}
				}
			}

			merr := d.mountUtil.MountVolume(r.Name, state.mountpoint)
			if merr != nil {
				logrus.Errorf("failed to mount the volume: %v", merr)
				return volume.Response{Err: fmt.Sprintf("failed to mount the volume: %v", merr)}
			}
		}

		return volume.Response{
			Mountpoint: state.mountpoint,
		}
	}

	logrus.Errorf("volume named '%v' not found", r.Name)
	return volume.Response{Err: fmt.Sprintf("volume named '%v' not found", r.Name)}
}

func (d Driver) Unmount(r volume.UnmountRequest) volume.Response {
	logrus.Infof("[Unmount]: %+v", r)

	d.m.Lock()
	defer d.m.Unlock()

	if state, ok := d.volumes[r.Name]; ok {
		state.referenceCount--

		if state.referenceCount == 0 {
			logrus.Info("unmounting the volume since it is not referenced anymore")

			merr := d.mountUtil.UnmountVolume(r.Name, state.mountpoint)
			if merr != nil {
				logrus.Errorf("failed to unmount the volume: %v", merr)
				return volume.Response{Err: fmt.Sprintf("failed to unmount the volume: %v", merr)}
			}
		}
	}

	return volume.Response{}
}

func (d Driver) Capabilities(r volume.Request) volume.Response {
	logrus.Infof("[Capabilities]: %+v", r)

	return volume.Response{
		Capabilities: volume.Capability{Scope: "local"},
	}
}

func (d Driver) initVolumesFromMetadata() error {
	metadataFiles, ferr := ioutil.ReadDir(d.baseMetadataPath)
	if ferr != nil {
		return ferr
	}

	for _, metadataFile := range metadataFiles {
		volumeName := metadataFile.Name()
		metadataFilePath := filepath.Join(d.baseMetadataPath, volumeName)

		logrus.Infof("Initializing volume '%v' from metadata file '%v'", volumeName, metadataFilePath)

		volumeState, ierr := d.initVolume(volumeName)
		if ierr != nil {
			return ierr
		}

		d.volumes[volumeName] = volumeState
	}

	return nil
}

func (d Driver) initVolume(name string) (*VolumeState, error) {
	doVolume := d.doFacade.GetVolumeByRegionAndName(d.region, name)
	if doVolume == nil {
		logrus.Errorf("DigitalOcean volume not found for region '%v' and name '%v'", d.region, name)
		return nil, fmt.Errorf("DigitalOcean volume not found for region '%v' and name '%v'", d.region, name)
	}

	volumePath := filepath.Join(d.baseMountPath, name)

	merr := os.MkdirAll(volumePath, MountDirMode)
	if merr != nil {
		logrus.Errorf("failed to create the volume mount path '%v'", volumePath)
		return nil, fmt.Errorf("failed to create the volume mount path '%v'", volumePath)
	}

	d.mountUtil.UnmountVolume(name, volumePath)

	volumeState := &VolumeState{
		doVolumeID:     doVolume.ID,
		mountpoint:     volumePath,
		referenceCount: 0,
	}

	return volumeState, nil
}
