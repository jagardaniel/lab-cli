# lab-cli
This is a simple tool to create libvirt VMs with a preseed/kickstart config from the network. It also configures a user ready for Ansible use. The goal with this "project" is to get more familiar with programming.

## Usage

### Configuration
Copy the config directory in this repository to `$XDG_CONFIG_HOME/lab-cli` (if set) or `$HOME/.config/lab-cli`

```bash
$ ls /home/daniel/.config/lab-cli/
config.toml  templates
```

### Create VM
Create a new Debian VM with the name lab01
```bash
$ labcli create lab01
```

Create a CentOS VM called lab02 with 20GB disk size and member of the Ansible groups webservers and dbservers
```bash
$ labcli create --distro centos --disk 20 --groups webservers,dbservers lab02
```

### Remove VM
```bash
$ labcli remove lab01
```

### List VMs
```bash
$ labcli list
```

### Start or stop a VM
```bash
$ labcli stop web01
$ labcli start web02
```

### SSH into a VM
```bash
$ labcli ssh web01
```

## Requirements

## Build and installation

## Configuration
