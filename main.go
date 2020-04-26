// This is a simple tool to create libvirt VMs with a preseed/kickstart config from the network.
// It creates a user for Ansible, adds the public key to the users authorized_keys and configure sudo.
// You can then use a dynamic inventory script in Ansible to find them and it should "just work".
//
// A separate network is created where all the VMs exists. The virt-install tool creates the actual VM
// and every VM will have a description with a prefix (to "mark" it), its IP address and the Ansible groups
// that it belongs to. The IP address is set automatically in the specified range and it will use the
// description of other VMs to check which IP addresses that are already being used.
//
// The goal with this little "project" is to learn programming. There are definitely tools out there that
// does the same thing but much better and easier. Everything is pretty messy and in one file right now.
// I always get stuck on "where should I put everything, how should I split up my files, what should be a method
// or function etc". So I usually give up before I actually do something. So this is an attempt to get
// something to work that can be improved on in the future.

package main

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/BurntSushi/toml"
	libvirt "libvirt.org/libvirt-go"
)

type NetworkConfig struct {
	Name       string `toml:"name"`
	Domain     string `toml:"domain"`
	BridgeName string `toml:"bridge_name"`
	Address    net.IP `toml:"address"`
	Netmask    net.IP `toml:"netmask"`
	RangeStart net.IP `toml:"range_start"`
	RangeEnd   net.IP `toml:"range_end"`
}

type DistroConfig struct {
	Location string `toml:"location"`
}

type Config struct {
	VirtInstallPath       string        `toml:"virt_install_path"`
	AnsiblePublicKey      string        `toml:"ansible_public_key"`
	AnsiblePrivateKeyPath string        `toml:"ansible_private_key_path"`
	Network               NetworkConfig `toml:"network"`
	Debian                DistroConfig  `toml:"debian"`
	Centos                DistroConfig  `toml:"centos"`
}

type GeneralOptions struct {
	Name string
}

type CreateOptions struct {
	Name   string
	Distro string
	RAM    int
	VCPUs  int
	Disk   int
	Groups []string
}

type NetworkBridge struct {
	Name string `xml:"name,attr"`
}

type NetworkIP struct {
	Address net.IP `xml:"address,attr"`
	Netmask net.IP `xml:"netmask,attr"`
}

type NetworkXML struct {
	XMLName xml.Name      `xml:"network"`
	Name    string        `xml:"name"`
	Forward string        `xml:"forward"`
	Bridge  NetworkBridge `xml:"bridge"`
	IP      NetworkIP     `xml:"ip"`
}

type DomainSummary struct {
	Name    string
	Address net.IP
	Groups  []string
	Status  bool
}

type GroupFlag []string

var defaultConfig = Config{
	VirtInstallPath:       "/usr/bin/virt-install",
	AnsiblePublicKey:      "",
	AnsiblePrivateKeyPath: "~/.ssh/labcli_private",
	Network: NetworkConfig{
		Name:       "labnet",
		Domain:     "lab.local",
		BridgeName: "virbr100",
		Address:    net.ParseIP("192.168.100.1"),
		Netmask:    net.ParseIP("255.255.255.0"),
		RangeStart: net.ParseIP("192.168.100.10"),
		RangeEnd:   net.ParseIP("192.168.100.200"),
	},
	Debian: DistroConfig{
		Location: "http://ftp.se.debian.org/debian/dists/buster/main/installer-amd64/",
	},
	Centos: DistroConfig{
		Location: "http://mirror.nsc.liu.se/CentOS/8/BaseOS/x86_64/kickstart/",
	},
}

func main() {
	if len(os.Args) < 2 {
		exitError(errors.New("you must specify a subcommand"))
	}

	// Get configuration location
	configDir, err := getConfigDir()
	if err != nil {
		exitError(err)
	}

	configFile := path.Join(configDir, "config.toml")

	// Load configuration from file
	config, err := loadConfig(configFile)
	if err != nil {
		exitError(err)
	}

	switch os.Args[1] {
	case "create":
		err := createCommand(os.Args, config)
		if err != nil {
			exitError(err)
		}
	case "remove":
		err := removeCommand(os.Args)
		if err != nil {
			exitError(err)
		}
	case "list":
		err := listCommand()
		if err != nil {
			exitError(err)
		}
	case "start":
		err := actionCommand(os.Args, "start")
		if err != nil {
			exitError(err)
		}
	case "stop":
		err := actionCommand(os.Args, "stop")
		if err != nil {
			exitError(err)
		}
	case "ssh":
		err := sshCommand(os.Args, config)
		if err != nil {
			exitError(err)
		}
	default:
		exitError(fmt.Errorf("'%s' is not a valid subcommand", os.Args[1]))
	}
}

func createCommand(args []string, config *Config) error {
	// Parse arguments
	options, err := parseCreate(args)
	if err != nil {
		return err
	}

	// Create a libvirt connection
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return err
	}

	// Check if a VM with the same name already exists
	domain, err := getDomain(conn, options.Name)
	if domain != nil {
		return fmt.Errorf("'%s' already exists", options.Name)
	}

	// Also return with an error if something else went wrong (except not found)
	if err != nil {
		if !strings.Contains(err.Error(), "Domain not found") {
			return err
		}
	}

	// Create or get existing network
	network, err := getNetwork(conn, config)
	if err != nil {
		// Create the network if it does not exist
		if strings.Contains(err.Error(), "Network not found") {
			network, err = createNetwork(conn, config)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Make sure the network is running
	err = startNetwork(conn, network)
	if err != nil {
		return err
	}

	// Find next available IP address
	addr, err := nextAvailableAddress(conn, config)
	if err != nil {
		return err
	}

	// Create a temporary directory where we will save our parsed template file
	outDir, err := ioutil.TempDir("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(outDir)

	// Render file from our template into our temporary directory
	outFile, err := renderTemplate(config, options, outDir, addr)
	if err != nil {
		return err
	}

	description := fmt.Sprintf("description=labcli:%s:%s", addr, strings.Join(options.Groups, ","))

	// Prepare arguments for virt-install
	// Maybe we can do this without virt-install in the future
	arguments := []string{
		"--connect", "qemu:///system",
		"--name", options.Name,
		"--ram", strconv.Itoa(options.RAM),
		"--vcpus", strconv.Itoa(options.VCPUs),
		"--disk", fmt.Sprintf("size=%s", strconv.Itoa(options.Disk)),
		"--network", fmt.Sprintf("network=%s", config.Network.Name),
		"--metadata", description,
		"--noautoconsole",
		"--initrd-inject", outFile,
	}

	// Add extra arguments based on distro selection
	if options.Distro == "debian" {
		arguments = append(
			arguments,
			"--extra-args", "auto",
			"--location", config.Debian.Location,
		)
	} else if options.Distro == "centos" {
		arguments = append(
			arguments,
			"--extra-args", fmt.Sprintf("inst.ks=file:/%s", filepath.Base(outFile)),
			"--location", config.Centos.Location,
		)
	}

	// Run virt-install with our arguments, combine stdout/stderr
	// Make the output a little bit better in the future, we don't need to print everything from virt-install
	output, err := exec.Command(config.VirtInstallPath, arguments...).CombinedOutput()
	if err != nil {
		return err
	}

	fmt.Println(string(output))

	fmt.Printf("\n'%s' is hopefully being installed right now. After the installation is finished the VM will shut down and you have to start it manually.\n", options.Name)

	return nil
}

func removeCommand(args []string) error {
	// Parse arguments
	options, err := parseGeneral(args, "remove")
	if err != nil {
		return err
	}

	// Create a libvirt connection
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return err
	}

	// Check if the VM exists
	domain, err := getDomain(conn, options.Name)
	if err != nil {
		// If we get an error that the domain does not exist
		if strings.Contains(err.Error(), "Domain not found") {
			return fmt.Errorf("'%s' does not exist", options.Name)
		}

		return err
	}

	// We need a way to remove the VM disk and we can't do it manually because
	// of the the file permission. There is probably a more beautiful way to
	// link a volume with the VM but here we will parse the VMs XML configuration to
	// find the disk path. Then find the volume object by the disk path and remove it.
	type DomainXML struct {
		Devices struct {
			Disk struct {
				Source struct {
					File string `xml:"file,attr"`
				} `xml:"source"`
			} `xml:"disk"`
		} `xml:"devices"`
	}

	xmlDesc, err := domain.GetXMLDesc(0)
	if err != nil {
		return err
	}

	var parsedDomain DomainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &parsedDomain); err != nil {
		return err
	}

	// Force stop the VM if it is running
	active, err := domain.IsActive()
	if err != nil {
		return err
	}

	if active {
		err = domain.DestroyFlags(libvirt.DOMAIN_DESTROY_DEFAULT)
		if err != nil {
			return err
		}
	}

	// Remove the VM
	err = domain.Undefine()
	if err != nil {
		return err
	}

	// Find volume by file path
	volume, err := conn.LookupStorageVolByPath(parsedDomain.Devices.Disk.Source.File)
	if err != nil {
		return err
	}

	// Remove the volume
	err = volume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL)
	if err != nil {
		return err
	}

	fmt.Printf("'%s' has been removed\n", options.Name)

	return nil
}

func listCommand() error {
	// Create a libvirt connection
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 8, 2, '\t', tabwriter.AlignRight)
	fmt.Fprintln(writer, "Name\tRunning\tIP Address\tAnsible groups")

	domains, err := getAllDomains(conn)
	if err != nil {
		return err
	}

	for _, domain := range domains {
		summary, err := getDomainSummary(&domain)
		if err != nil {
			return err
		}

		groups := strings.Join(summary.Groups, ", ")
		fmt.Fprintf(writer, "%s\t%v\t%s\t%s\n", summary.Name, summary.Status, summary.Address, groups)
	}

	writer.Flush()

	return nil
}

func actionCommand(args []string, action string) error {
	// Parse arguments
	var options *GeneralOptions
	var err error

	if action == "start" {
		options, err = parseGeneral(args, "start")
	} else if action == "stop" {
		options, err = parseGeneral(args, "stop")
	}

	if err != nil {
		return err
	}

	// Create a libvirt connection
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return err
	}

	// Check if the VM exists
	domain, err := getDomain(conn, options.Name)
	if err != nil {
		// If we get an error that the domain does not exist
		if strings.Contains(err.Error(), "Domain not found") {
			return fmt.Errorf("'%s' does not exist", options.Name)
		}

		return err
	}

	// Check current status
	active, err := domain.IsActive()
	if err != nil {
		return err
	}

	if action == "start" {
		if active {
			return fmt.Errorf("'%s' is already running", options.Name)
		}

		err = domain.Create()
		if err != nil {
			return err
		}
	} else if action == "stop" {
		if !active {
			return fmt.Errorf("'%s' is already stopped", options.Name)
		}

		err = domain.Destroy()
		if err != nil {
			return err
		}
	}

	return nil
}

func sshCommand(args []string, config *Config) error {
	// Parse arguments
	options, err := parseGeneral(args, "ssh")
	if err != nil {
		return err
	}

	// Create a libvirt connection
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return err
	}

	// Check if the VM exists
	domain, err := getDomain(conn, options.Name)
	if err != nil {
		// If we get an error that the domain does not exist
		if strings.Contains(err.Error(), "Domain not found") {
			return fmt.Errorf("'%s' does not exist", options.Name)
		}

		return err
	}

	// Make sure the VM is running
	active, err := domain.IsActive()
	if err != nil {
		return err
	}

	if !active {
		return fmt.Errorf("'%s' is not running", options.Name)
	}

	// Get IP address
	summary, err := getDomainSummary(domain)
	if err != nil {
		return err
	}

	// Prepare arguments for SSH
	arguments := []string{
		"-q",
		"-o", "StrictHostKeyChecking=no",
		"-i", config.AnsiblePrivateKeyPath,
		fmt.Sprintf("ansible@%s", summary.Address),
	}

	// Run SSH. This is probably possible to do with golangs crypto/ssh package instead
	// which would be a better solution
	cmd := exec.Command("/usr/bin/ssh", arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Run()

	return nil
}

func getDomain(conn *libvirt.Connect, name string) (*libvirt.Domain, error) {
	domain, err := conn.LookupDomainByName(name)
	if err != nil {
		return nil, err
	}

	return domain, nil
}

func getAllDomains(conn *libvirt.Connect) ([]libvirt.Domain, error) {
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, err
	}

	var listDomains []libvirt.Domain

	for _, domain := range domains {
		domainDesc, err := getDomainDesc(&domain)
		if err != nil {
			return nil, err
		}

		// We only care about VMs that we "manage"
		if strings.HasPrefix(domainDesc, "labcli:") {
			listDomains = append(listDomains, domain)
		}
	}

	return listDomains, nil
}

func getDomainDesc(domain *libvirt.Domain) (string, error) {
	type DomainXML struct {
		Description string `xml:"description"`
	}

	// Get XML description of the domain
	xmlDesc, err := domain.GetXMLDesc(0)
	if err != nil {
		return "", err
	}

	var parsedDesc DomainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &parsedDesc); err != nil {
		return "", err
	}

	return parsedDesc.Description, nil
}

func getDomainSummary(domain *libvirt.Domain) (*DomainSummary, error) {
	desc, err := getDomainDesc(domain)
	if err != nil {
		return nil, err
	}

	descSplit := strings.Split(desc, ":")[1:]

	if len(descSplit) != 2 {
		return nil, errors.New("something went wrong parsing the domain description")
	}

	// Get name
	name, err := domain.GetName()
	if err != nil {
		return nil, err
	}

	// Get status
	status, err := domain.IsActive()
	if err != nil {
		return nil, err
	}

	// Get IP and groups from our parsed description
	ip := net.ParseIP(descSplit[0])
	groups := strings.Split(descSplit[1], ",")

	domainSum := &DomainSummary{
		Name:    name,
		Address: ip,
		Groups:  groups,
		Status:  status,
	}

	return domainSum, nil
}

func getNetwork(conn *libvirt.Connect, config *Config) (*libvirt.Network, error) {
	network, err := conn.LookupNetworkByName(config.Network.Name)
	if err != nil {
		return nil, err
	}

	return network, nil
}

func createNetwork(conn *libvirt.Connect, config *Config) (*libvirt.Network, error) {
	data := NetworkXML{
		Name: config.Network.Name,
		Bridge: NetworkBridge{
			Name: config.Network.BridgeName,
		},
		IP: NetworkIP{
			Address: config.Network.Address,
			Netmask: config.Network.Netmask,
		},
	}

	// Create XML structure
	xmlData, err := xml.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Define network in libvirt
	network, err := conn.NetworkDefineXML(string(xmlData))
	if err != nil {
		return nil, err
	}

	// Set network to autostart
	err = network.SetAutostart(true)
	if err != nil {
		return nil, err
	}

	return network, nil
}

func statusNetwork(conn *libvirt.Connect, network *libvirt.Network) (bool, error) {
	status, err := network.IsActive()
	if err != nil {
		return false, err
	}

	return status, nil
}

func startNetwork(conn *libvirt.Connect, network *libvirt.Network) error {
	// Check current network status
	status, err := statusNetwork(conn, network)
	if err != nil {
		return err
	}

	// Network is inactive, start it
	if !status {
		err := network.Create()
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *GroupFlag) String() string {
	// I don't understand where this is being used, but required to satisfy the interface(?)
	return ""
}

func (g *GroupFlag) Set(value string) error {
	groups := strings.Split(value, ",")

	for _, group := range groups {
		*g = append(*g, group)
	}

	return nil
}

func parseCreate(args []string) (*CreateOptions, error) {
	var groups GroupFlag
	command := flag.NewFlagSet("create", flag.ExitOnError)
	ram := command.Int("ram", 2048, "ram help")
	distro := command.String("distro", "debian", "distribution help")
	vcpus := command.Int("vcpus", 2, "VCPUs help")
	disk := command.Int("disk", 10, "disk help")
	command.Var(&groups, "groups", "groups help")

	command.Parse(args[2:])

	if len(command.Args()) < 1 {
		return nil, errors.New("create subcommand requires a name")
	}

	// Validate distro selection
	if *distro != "debian" && *distro != "centos" {
		return nil, errors.New("selected distribution is not available")
	}

	// Set default group if not specified
	if len(groups) < 1 {
		groups = []string{"ungrouped"}
	}

	options := &CreateOptions{
		Name:   command.Args()[0],
		Distro: *distro,
		RAM:    *ram,
		VCPUs:  *vcpus,
		Disk:   *disk,
		Groups: groups,
	}

	return options, nil
}

func parseGeneral(args []string, cmd string) (*GeneralOptions, error) {
	command := flag.NewFlagSet(cmd, flag.ExitOnError)

	command.Parse(args[2:])

	if len(command.Args()) < 1 {
		return nil, fmt.Errorf("%s subcommand requires a name", cmd)
	}

	options := &GeneralOptions{Name: command.Args()[0]}

	return options, nil
}

func nextAvailableAddress(conn *libvirt.Connect, config *Config) (net.IP, error) {
	rangeStart := config.Network.RangeStart
	rangeEnd := nextAddress(config.Network.RangeEnd)

	// Get a list of all existing VMs
	domains, err := getAllDomains(conn)
	if err != nil {
		return nil, err
	}

	// Get next address within the range that is not already used
	var address net.IP

	for ip := rangeStart; !ip.Equal(rangeEnd); nextAddress(ip) {
		available := true

		for _, domain := range domains {
			summary, err := getDomainSummary(&domain)
			if err != nil {
				return nil, err
			}

			if ip.Equal(summary.Address) {
				available = false
			}
		}

		if available {
			address = ip
			break
		}
	}

	if address == nil {
		return nil, errors.New("could not find an available IP address")
	}

	return address, nil
}

func getConfigDir() (string, error) {
	// Check if a custom config directory is set (try to respect XDG spec)
	// otherwise default to .config in the users home directory
	configHome, exists := os.LookupEnv("XDG_CONFIG_HOME")
	if !exists {
		user, err := user.Current()

		if err != nil {
			return "", err
		}

		configHome = path.Join(user.HomeDir, ".config")
	}

	return path.Join(configHome, "lab-cli"), nil
}

func getTemplateDir() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}

	return path.Join(configDir, "templates"), nil
}

func loadConfig(configFile string) (*Config, error) {
	// Make sure that the config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return nil, errors.New("Config file does not exist")
	} else if err != nil {
		return nil, err
	}

	// Values from the config file will overwrite our default values
	config := defaultConfig
	if _, err := toml.DecodeFile(configFile, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func renderTemplate(config *Config, options *CreateOptions, outDir string, address net.IP) (string, error) {
	type Template struct {
		Hostname   string
		Domain     string
		Address    net.IP
		Netmask    net.IP
		Gateway    net.IP
		AnsibleKey string
	}

	tmpl := Template{
		Hostname:   options.Name,
		Domain:     config.Network.Domain,
		Address:    address,
		Netmask:    config.Network.Netmask,
		Gateway:    config.Network.Address,
		AnsibleKey: config.AnsiblePublicKey,
	}

	// Select distro configuration and set static names on the output file
	// since Debian seems to require the preseed config to be named "preseed.cfg"
	var outName string

	if options.Distro == "debian" {
		outName = "preseed.cfg"
	} else if options.Distro == "centos" {
		outName = "kickstart.cfg"
	}

	templateDir, err := getTemplateDir()
	if err != nil {
		return "", err
	}

	// Parse/render template file and create the output file in the temporary directory
	templateFile := path.Join(templateDir, fmt.Sprintf("%s.tmpl", outName))
	t, err := template.ParseFiles(templateFile)
	if err != nil {
		return "", err
	}

	outFile := path.Join(outDir, outName)
	f, err := os.Create(outFile)
	if err != nil {
		return "", err
	}

	err = t.Execute(f, tmpl)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	f.Close()

	return outFile, nil
}

// Get next IPv4 address - from a stackoverflow reply
func nextAddress(origAddress net.IP) net.IP {
	ip := origAddress.To4()

	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}

	return ip
}

// Exit the program and write error message to stderr
func exitError(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}
