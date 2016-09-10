package main

import (
	"os/exec"
)

const mountDevicePrefix = "/dev/disk/by-id/scsi-0DO_Volume_"

type MountUtil struct {
}

func NewMountUtil() *MountUtil {
	return &MountUtil{}
}

func (m MountUtil) MountVolume(volumeName string, mountpoint string) error {
	cmd := exec.Command("mount", mountDevicePrefix+volumeName, mountpoint)
	return cmd.Run()
}

func (m MountUtil) UnmountVolume(volumeName string, mountpoint string) error {
	cmd := exec.Command("umount", mountpoint)
	return cmd.Run()
}
