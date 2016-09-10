Docker Volume Driver for DigitalOcean
=====================================

This repo hosts the Docker Volume Driver for DigitalOcean. The driver is based on the [Docker Volume Plugin framework](https://docs.docker.com/engine/extend/plugins_volume/) and it integrates DigitalOcean's [block storage solution](https://www.digitalocean.com/community/tutorials/how-to-use-block-storage-on-digitalocean) into the Docker ecosystem by automatically attaching a given block storage volume to a DigitalOcean droplet and making the contents of the volume available to Docker containers running on that droplet.


## Download

The driver is written in Go and it consists of a single static binary which can be downloaded from the [releases](https://github.com/omallo/docker-volume-plugin-dostorage/releases) page. Appropriate binaries are made available for different Linux platforms and architectures.


## Installation

For installing the driver on a DigitalOcean droplet, you will need the following before proceeding with the subsequent steps:
- You need to have SSH access to your droplet. The subsequent commands should all be executed on the droplet's command line.
- You need to have an [API access token](https://cloud.digitalocean.com/settings/api/tokens) which is used by the driver to access the DigitalOcean REST API.

First, you have to download the driver's binary to the droplet and make it executable (make sure you download the binary for the appropriate release version and Linux platform/architecture):
```sh
curl \
  -o /usr/bin/docker-volume-plugin-dostorage \
  https://github.com/omallo/docker-volume-plugin-dostorage/releases/download/v0.1.0/docker-volume-plugin-dostorage_linux_amd64

chmod +x /usr/bin/docker-volume-plugin-dostorage
```

Once downloaded, the driver can be started in the background as follows by providing your DigitalOcean API access token:
```sh
docker-volume-plugin-dostorage --access-token=<your-API-Access-Token> &
```

Docker plugins should usually be started before the Docker engine so it is advisable to restart the Docker engine after installing the driver. Depending on your Linux distribution, this can be done using either the `service` command
```sh
service docker restart
```
or the `systemctl` command
```sh
systemctl restart docker
```

You are now ready to use the driver for your Docker containers!


## Basic Usage

Before using the driver for your Docker containers, you must create a [DigitalOcean volume](https://cloud.digitalocean.com/droplets/volumes). For the subsequent steps, we assume a DigitalOcean volume named `myvol-01`. As of now, the driver does not support volumes with multiple partitions so it is assumed that the volume consists of a single partition which you might have created e.g. as follows:
```sh
sudo mkfs.ext4 -F /dev/disk/by-id/scsi-0DO_Volume_myvol-01
```
An in-depth description on how to create and format DigitalOcean volumes can be found [here](https://www.digitalocean.com/community/tutorials/how-to-use-block-storage-on-digitalocean). Please note that a DigitalOcean volume must be created and formatted manually before it can be integrated into Docker using the driver.

Once you have created and formatted your DigitalOcean volume, you can create a Docker volume using the same name (assuming a DigitalOcean volume named `myvol-01`):
```sh
docker volume create --driver dostorage --name myvol-01
```

Once the Docker volume was created, you can use it for your containers. E.g. you can  list the contents of your DigitalOcean volume by mapping it to the container path `/mydata` as follows:
```sh
docker run --rm --volume myvol-01:/mydata busybox ls -la /mydata
```

You can also start an interactive shell on your container and access the contents of your DigitalOcean volume from within your container:
```sh
docker run -it --volume myvol-01:/mydata busybox sh

# the following commands are executed within the container's shell
ls -la /mydata
echo "hello world" >/mydata/greeting.txt
cat /mydata/greeting.txt
exit
```
Since all the changes made within the cotnainer's `/mydata` path are performed on the DigitalOcean volume storage device, you will not loose the changes even if you later attach the DigitalOcean volume to a different droplet.

The current status of the Docker volume can be inspected using the following command:
```sh
docker volume inspect myvol-01
```

The inspection command will return a result similar to the following:
```json
[
    {
        "Name": "myvol-01",
        "Driver": "dostorage",
        "Mountpoint": "/mnt/dostorage/myvol-01",
        "Status": {
            "AttachedDropletIDs": [
                2.5355869e+07
            ],
            "ReferenceCount": 0,
            "VolumeID": "0b3aef8c-7767-11e6-a7c4-000f53315860"
        },
        "Labels": {},
        "Scope": "local"
    }
]
```

Apart from the standard inspection information like the local mountpoint path, the result contains a `Status` field with the following information (the status field is only supported with Docker version >=1.12.0):
- `VolumeID`: The ID of the DigitalOcean volume.
- `AttachedDropletIDs`: The IDs of the droplets to which the DigitalOcean volume is currently attached (at most 1).
- `ReferenceCount`: The number of running Docker containers which are using the volume.


## Docker Swarm Usage

If you use Docker in [swarm mode](https://docs.docker.com/engine/swarm/) with a cluster of droplets, you can use the driver in very much the same way as with a single droplet. The following things should be considered when using a DigitalOcean volume in a Docker cluster:
- The Docker volume must be created on every Docker host separately (using `docker volume create` as described above).
- The driver takes care of attaching a DigitalOcean volume to the appropriate droplet when you start a container which uses that volume on the droplet (and possibly detaching it from any other droplet).
- A DigitalOcean volume can only be attached to a single droplet at the same time. For that reason, you must not run Docker containers concurrently on different hosts which use the same DigitalOcean volume.


## Logging

The driver logs to the STDOUT as well as to the local `syslog` instance (if supported). Syslog logging uses the `dostorage` tag.


## Systemd Integration

It is advisable to use `systemd` to manage the startup and shutdown of the driver. Details on how to configure `systemd` for a Docker plugin (including socket activation), can be found [here](https://docs.docker.com/engine/extend/plugin_api/).
