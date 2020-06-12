# lab-cli
This is a simple tool to create libvirt VMs with a preseed/kickstart config from the network. It also configures a user ready for Ansible use. The goal with this small project is to get more familiar with programming.

## Requirements
* Go
* libvirt (KVM/QEMU)
* virt-install

The user running lab-cli needs to connect to the system libvirtd instance. The easiest way is to add your user to the libvirt group.

You can test if this works by running this command as your user
```bash
$ virsh -c qemu:///system
```


## Installation

Development files for libvirt is required to build the application (required by libvirt-go). The package is usually called `libvirt-dev` or something similar.

Example:
```bash
# openSUSE
$ zypper install libvirt-devel

# Debian
$ apt install libvirt-dev
```

The simplest way to install the application is to run
```bash
$ go get -u github.com/jagardaniel/lab-cli
```

It will install the lab-cli binary to $GOPATH/bin or $HOME/go/bin if the GOPATH environment variable is not set.


## Configuration
Copy the config directory from the repository to `$HOME/.config/lab-cli` or `$XDG_CONFIG_HOME/lab-cli` if XDG_CONFIG_HOME is set.

```bash
$ ls /home/daniel/.config/lab-cli/
config.toml  templates
```

## Usage

### Create VM
Create a new Debian VM with the name lab01
```bash
$ lab-cli create lab01
```

Create a CentOS VM called lab02 with 20GB disk size and member of the Ansible groups webservers and dbservers
```bash
$ lab-cli create --distro centos --disk 20 --groups webservers,dbservers lab02
```

The installation takes a while to complete (~5 minutes) and the VM will shut down when the installation is finished. It should reboot after installation but... yeah, that doesn't right now. You can use a tool like virt-manager to see how the installation is going.


### Remove VM
```bash
$ lab-cli remove lab01
```

### List VMs
```bash
$ lab-cli list
```

### Start or stop a VM
```bash
$ lab-cli stop web01
$ lab-cli start web02
```

### SSH into a VM
```bash
$ lab-cli ssh web01
```
