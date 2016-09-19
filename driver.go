package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const (
	DriverName    = "dostorage"
	DriverVersion = "0.3.0-SNAPSHOT"
)

type Driver struct {
	region        string
	dropletID     int
	volumes       map[string]*VolumeState
	baseMountPath string
	doFacade      *DoFacade
	mountUtil     *MountUtil
	m             *sync.Mutex
}

type VolumeState struct {
	doVolumeID     string
	mountpoint     string
	referenceCount int
}

func NewDriver(doFacade *DoFacade, mountUtil *MountUtil, baseMountPath string) (*Driver, error) {
	logrus.Info("creating a new driver instance")

	region, rerr := doFacade.GetLocalRegion()
	if rerr != nil {
		return nil, rerr
	}

	dropletID, derr := doFacade.GetLocalDropletID()
	if derr != nil {
		return nil, derr
	}

	merr := os.MkdirAll(baseMountPath, os.ModeDir)
	if merr != nil {
		return nil, merr
	}

	logrus.Infof("droplet metadata: region='%v', dropletID=%v", region, dropletID)

	return &Driver{
		region:        region,
		dropletID:     dropletID,
		volumes:       make(map[string]*VolumeState),
		baseMountPath: baseMountPath,
		doFacade:      doFacade,
		mountUtil:     mountUtil,
		m:             &sync.Mutex{},
	}, nil
}

func (d Driver) Create(r volume.Request) volume.Response {
	logrus.Infof("[Create]: %+v", r)

	d.m.Lock()
	defer d.m.Unlock()

	doVolume := d.doFacade.GetVolumeByRegionAndName(d.region, r.Name)
	if doVolume == nil {
		logrus.Errorf("DigitalOcean volume not found for region '%v' and name '%v'", d.region, r.Name)
		return volume.Response{Err: fmt.Sprintf("DigitalOcean volume not found for region '%v' and name '%v'", d.region, r.Name)}
	}

	volumePath := filepath.Join(d.baseMountPath, r.Name)

	merr := os.MkdirAll(volumePath, os.ModeDir)
	if merr != nil {
		logrus.Errorf("failed to create the volume mount path '%v'", volumePath)
		return volume.Response{Err: fmt.Sprintf("failed to create the volume mount path '%v'", volumePath)}
	}

	d.mountUtil.UnmountVolume(r.Name, volumePath)

	d.volumes[r.Name] = &VolumeState{
		doVolumeID:     doVolume.ID,
		mountpoint:     volumePath,
		referenceCount: 0,
	}

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
