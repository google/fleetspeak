package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"time"

	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	ccpb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
	fcpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	gspb "github.com/google/fleetspeak/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	anypb "google.golang.org/protobuf/types/known/anypb"
	dpb "google.golang.org/protobuf/types/known/durationpb"
)

type serverInfo struct {
	serverCmd       *exec.Cmd
	serviceCmd      *exec.Cmd
	httpsListenPort int
}

// ComponentsInfo contains IDs of connected clients and exec.Cmds for all components
type ComponentsInfo struct {
	masterServerCmd *exec.Cmd
	balancerCmd     *exec.Cmd
	servers         []serverInfo
	clientCmds      []*exec.Cmd
	ClientIDs       []string
}

// KillAll kills all running processes
func (cc *ComponentsInfo) KillAll() {
	for _, cl := range cc.clientCmds {
		if cl != nil {
			cl.Process.Kill()
			cl.Wait()
		}
	}
	for _, s := range cc.servers {
		if s.serverCmd != nil {
			s.serverCmd.Process.Kill()
			s.serverCmd.Wait()
		}
		if s.serviceCmd != nil {
			s.serviceCmd.Process.Kill()
			s.serviceCmd.Wait()
		}
	}
	if cc.masterServerCmd != nil {
		cc.masterServerCmd.Process.Kill()
		cc.masterServerCmd.Wait()
	}
	if cc.balancerCmd != nil {
		cc.balancerCmd.Process.Kill()
		cc.balancerCmd.Wait()
	}
}

// Starts a command and redirects its output to main stdout
func startCommand(cmd *exec.Cmd) error {
	stdoutIn, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("cmd.Start() failed: %v", err)
	}

	go func() {
		io.Copy(os.Stdout, stdoutIn)
	}()
	go func() {
		io.Copy(os.Stderr, stderrIn)
	}()
	return nil
}

func getNewClientIDs(admin sgrpc.AdminClient, startTime time.Time) ([]string, error) {
	var ids [][]byte
	ctx := context.Background()
	res, err := admin.ListClients(ctx,
		&spb.ListClientsRequest{ClientIds: ids},
		grpc.MaxCallRecvMsgSize(1024*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("ListClients RPC failed: %v", err)
	}

	var newClients []string
	for _, cl := range res.Clients {
		if !cl.LastContactTime.IsValid() {
			continue
		}
		lastContactTime := cl.LastContactTime.AsTime()
		if lastContactTime.After(startTime) {
			id, err := common.BytesToClientID(cl.ClientId)
			if err != nil {
				return nil, fmt.Errorf("Invalid clientID: %v", err)
			}
			newClients = append(newClients, fmt.Sprintf("%v", id))
		}
	}
	return newClients, nil
}

// WaitForNewClientIDs waits and returns IDs of numClients clients
// connected to adminAddress after startTime
func WaitForNewClientIDs(adminAddress string, startTime time.Time, numClients int) ([]string, error) {
	conn, err := grpc.Dial(adminAddress, grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to fleetspeak admin interface [%v]: %v", adminAddress, err)
	}
	admin := sgrpc.NewAdminClient(conn)
	ids := make([]string, 0)
	for i := range 10 {
		if i > 0 {
			time.Sleep(time.Second)
		}
		ids, err = getNewClientIDs(admin, startTime)
		if err != nil {
			continue
		}
		if len(ids) > numClients {
			return nil, fmt.Errorf("Too many clients connected (expected: %v, connected: %v)", numClients, len(ids))
		}
		if len(ids) == numClients {
			return ids, nil
		}
	}
	return ids, errors.New("Not all clients connected")
}

// MysqlCredentials contains username, password and database
type MysqlCredentials struct {
	Host     string
	Username string
	Password string
	Database string
}

func buildBaseConfiguration(configDir string, mysqlCredentials MysqlCredentials, frontendAddr string) error {
	config := &cpb.Config{}
	config.ConfigurationName = "FleetspeakSetup"

	config.ComponentsConfig = new(fcpb.Config)
	config.ComponentsConfig.MysqlDataSourceName =
		fmt.Sprintf("%v:%v@tcp(%v)/%v", mysqlCredentials.Username, mysqlCredentials.Password, mysqlCredentials.Host, mysqlCredentials.Database)

	config.ComponentsConfig.HttpsConfig = new(fcpb.HttpsConfig)
	config.ComponentsConfig.HttpsConfig.ListenAddress = "localhost:6060"
	config.ComponentsConfig.HttpsConfig.DisableStreaming = false

	config.ComponentsConfig.AdminConfig = new(fcpb.AdminConfig)
	config.ComponentsConfig.AdminConfig.ListenAddress = "localhost:6061"

	config.PublicHostPort = append(config.PublicHostPort, frontendAddr)

	config.ServerComponentConfigurationFile = path.Join(configDir, "server.config")
	config.TrustedCertFile = path.Join(configDir, "trusted_cert.pem")
	config.TrustedCertKeyFile = path.Join(configDir, "trusted_cert_key.pem")
	config.ServerCertFile = path.Join(configDir, "server_cert.pem")
	config.ServerCertKeyFile = path.Join(configDir, "server_cert_key.pem")
	config.LinuxClientConfigurationFile = path.Join(configDir, "linux_client.config")
	config.WindowsClientConfigurationFile = path.Join(configDir, "windows_client.config")
	config.DarwinClientConfigurationFile = path.Join(configDir, "darwin_client.config")

	builtConfiguratorConfigPath := path.Join(configDir, "configurator.config")
	b, err := prototext.Marshal(config)
	if err != nil {
		return fmt.Errorf("Unable to marshal config: %v", err)
	}
	err = ioutil.WriteFile(builtConfiguratorConfigPath, b, 0644)
	if err != nil {
		return fmt.Errorf("Unable to write configurator file: %v", err)
	}

	// Build fleetspeak configurations
	_, err = exec.Command("fleetspeak/src/config/fleetspeak_config", "-config", builtConfiguratorConfigPath).Output()
	if err != nil {
		return fmt.Errorf("Failed to build Fleetspeak configurations: %v", err)
	}

	// Client services configuration
	clientServiceConfig := &fspb.ClientServiceConfig{Name: "FRR", Factory: "Daemon"}
	payload := &dspb.Config{}
	payload.Argv = append(payload.Argv, "python", "frr_python/frr_client.py")
	clientServiceConfig.Config, err = anypb.New(payload)
	if err != nil {
		return fmt.Errorf("Failed to marshal client service configuration: %v", err)
	}
	err = os.Mkdir(path.Join(configDir, "textservices"), 0777)
	if err != nil {
		return fmt.Errorf("Unable to create textservices directory: %v", err)
	}
	err = os.Mkdir(path.Join(configDir, "services"), 0777)
	if err != nil {
		return fmt.Errorf("Unable to create services directory: %v", err)
	}
	_, err = os.Create(path.Join(configDir, "communicator.txt"))
	if err != nil {
		return fmt.Errorf("Unable to create communicator.txt: %v", err)
	}
	b, err = prototext.Marshal(clientServiceConfig)
	if err != nil {
		return fmt.Errorf("Unable to marshal clientServiceConfig: %v", err)
	}
	err = ioutil.WriteFile(path.Join(configDir, "textservices", "frr.textproto"), b, 0644)
	if err != nil {
		return fmt.Errorf("Unable to write frr.textproto file: %v", err)
	}
	return nil
}

type fleetspeakServerConfigs struct {
	host               string
	frontendPort       int
	adminPort          int
	useHealthCheck     bool
	healthCheckPort    int
	httpsListenAddress string
	notificationPort   int
	useProxyProtocol   bool
}

func modifyFleetspeakServerConfig(configDir string, fsServerConfigs fleetspeakServerConfigs, newServerConfigPath, newServerServicesConfigPath string) error {
	// Update server addresses
	serverBaseConfigurationPath := path.Join(configDir, "server.config")

	serverConfig := &fcpb.Config{}
	serverConfigData, err := ioutil.ReadFile(serverBaseConfigurationPath)
	if err != nil {
		return fmt.Errorf("Unable to read server.config: %v", err)
	}
	err = prototext.Unmarshal(serverConfigData, serverConfig)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal server.config: %v", err)
	}

	serverConfig.NotificationListenAddress = fmt.Sprintf("%v:%v", fsServerConfigs.host, fsServerConfigs.notificationPort)
	serverConfig.HttpsConfig.ListenAddress = fsServerConfigs.httpsListenAddress
	serverConfig.AdminConfig.ListenAddress = fmt.Sprintf("%v:%v", fsServerConfigs.host, fsServerConfigs.adminPort)
	if fsServerConfigs.useHealthCheck {
		serverConfig.HealthCheckConfig = new(fcpb.HealthCheckConfig)
		serverConfig.HealthCheckConfig.ListenAddress = fmt.Sprintf("0.0.0.0:%v", fsServerConfigs.healthCheckPort)
	}
	if fsServerConfigs.useProxyProtocol {
		serverConfig.ProxyProtocol = true
	}
	b, err := prototext.Marshal(serverConfig)
	if err != nil {
		return fmt.Errorf("Unable to marshal serverConfig: %v", err)
	}
	err = ioutil.WriteFile(newServerConfigPath, b, 0644)
	if err != nil {
		return fmt.Errorf("Unable to write serverConfig to file: %w", err)
	}
	// Server services configuration
	serverServiceConf := spb.ServiceConfig{Name: "FRR", Factory: "GRPC"}
	grpcConfig := &gspb.Config{Target: fmt.Sprintf("%v:%v", fsServerConfigs.host, fsServerConfigs.frontendPort), Insecure: true}
	serviceConfig, err := anypb.New(grpcConfig)
	if err != nil {
		return fmt.Errorf("Failed to marshal grpcConfig: %v", err)
	}
	serverServiceConf.Config = serviceConfig
	serverConf := &spb.ServerConfig{Services: []*spb.ServiceConfig{&serverServiceConf}, BroadcastPollTime: &dpb.Duration{Seconds: 1}}

	b, err = prototext.Marshal(serverConf)
	if err != nil {
		return fmt.Errorf("Unable to marshal serverConf: %v", err)
	}
	err = ioutil.WriteFile(newServerServicesConfigPath, b, 0644)
	if err != nil {
		return fmt.Errorf("Unable to write server.services.config: %v", err)
	}
	return nil
}

func modifyFleetspeakClientConfig(configDir string, httpsListenAddress string, newLinuxConfigPath, newStateFilePath, configDirForClient string) error {
	linuxBaseConfigPath := path.Join(configDir, "linux_client.config")

	clientConfig := &ccpb.Config{}
	clientConfigData, err := ioutil.ReadFile(linuxBaseConfigPath)
	if err != nil {
		return fmt.Errorf("Unable to read LinuxClientConfigurationFile: %v", err)
	}
	err = prototext.Unmarshal(clientConfigData, clientConfig)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal clientConfigData: %v", err)
	}
	clientConfig.GetFilesystemHandler().ConfigurationDirectory = configDirForClient
	clientConfig.GetFilesystemHandler().StateFile = newStateFilePath
	clientConfig.Server = []string{httpsListenAddress}
	_, err = os.Create(clientConfig.GetFilesystemHandler().StateFile)
	if err != nil {
		return fmt.Errorf("Failed to create client state file: %v", err)
	}
	b, err := prototext.Marshal(clientConfig)
	if err != nil {
		return fmt.Errorf("Unable to marshal clientConfig: %v", err)
	}
	err = ioutil.WriteFile(newLinuxConfigPath, b, 0644)
	if err != nil {
		return fmt.Errorf("Failed to update LinuxClientConfigurationFile: %v", err)
	}

	return nil
}

// BuildConfigurations builds Fleetspeak configuration files for provided servers and
// number of clients that are supposed to be started on different machines
func BuildConfigurations(configDir string, serverHosts []string, serverFrontendIP string, numClients int, mysqlCredentials MysqlCredentials) error {
	healthCheckPort := 8085
	httpsListenPort := 6060
	adminPort := httpsListenPort + 1
	frontendPort := httpsListenPort + 2
	notificationPort := httpsListenPort + 3
	serverFrontendAddr := fmt.Sprintf("%v:%v", serverFrontendIP, httpsListenPort)

	err := buildBaseConfiguration(configDir, mysqlCredentials, serverFrontendAddr)
	if err != nil {
		return fmt.Errorf("Failed to build base configuration: %v", err)
	}

	// Build server configs
	for i := range len(serverHosts) {
		serverConfigPath := path.Join(configDir, fmt.Sprintf("server%v.config", i))
		serverServicesConfigPath := path.Join(configDir, fmt.Sprintf("server%v.services.config", i))
		err := modifyFleetspeakServerConfig(
			configDir,
			fleetspeakServerConfigs{
				host:               serverHosts[i],
				frontendPort:       frontendPort,
				adminPort:          adminPort,
				useHealthCheck:     true,
				healthCheckPort:    healthCheckPort,
				httpsListenAddress: serverFrontendAddr,
				notificationPort:   notificationPort,
			},
			serverConfigPath,
			serverServicesConfigPath,
		)
		if err != nil {
			return fmt.Errorf("Failed to build FS server configurations: %v", err)
		}
	}

	// Build client configs
	linuxConfigPath := path.Join(configDir, "linux_client.config")
	err = modifyFleetspeakClientConfig(configDir, serverFrontendAddr, linuxConfigPath, "client.state", ".")
	if err != nil {
		return fmt.Errorf("Failed to build FS client configurations: %v", err)
	}
	return nil
}

func (cc *ComponentsInfo) start(configDir string, frontendAddress, msAddress string, numServers, numClients int) error {
	firstAdminPort := 6061

	// Start Master server
	cc.masterServerCmd = exec.Command("fleetspeak/src/e2etesting/frr_master_server_main/frr_master_server_main", "--listen_address", msAddress, "--admin_address", fmt.Sprintf("localhost:%v", firstAdminPort))
	startCommand(cc.masterServerCmd)

	// Start servers and their services
	var serverHosts string
	for i := range numServers {
		adminPort := firstAdminPort + i*4
		httpsListenPort := adminPort - 1
		serverHosts += fmt.Sprintf("localhost:%v\n", httpsListenPort)
		frontendPort := adminPort + 1
		notificationPort := adminPort + 2
		serverConfigPath := path.Join(configDir, fmt.Sprintf("server%v.config", i))
		serverServicesConfigPath := path.Join(configDir, fmt.Sprintf("server%v.services.config", i))
		err := modifyFleetspeakServerConfig(
			configDir,
			fleetspeakServerConfigs{
				host:               "localhost",
				frontendPort:       frontendPort,
				adminPort:          adminPort,
				useHealthCheck:     false,
				httpsListenAddress: fmt.Sprintf("localhost:%v", httpsListenPort),
				notificationPort:   notificationPort,
				useProxyProtocol:   true,
			},
			serverConfigPath,
			serverServicesConfigPath,
		)
		if err != nil {
			return fmt.Errorf("Failed to build FS server configurations: %v", err)
		}

		serverCmd := exec.Command("fleetspeak/src/server/server/server", "-logtostderr", "-components_config", serverConfigPath, "-services_config", serverServicesConfigPath)
		serviceCmd := exec.Command(
			"python",
			"frr_python/frr_server.py",
			fmt.Sprintf("--master_server_address=%v", msAddress),
			fmt.Sprintf("--fleetspeak_message_listen_address=localhost:%v", frontendPort),
			fmt.Sprintf("--fleetspeak_server=localhost:%v", adminPort))
		cc.servers = append(cc.servers, serverInfo{httpsListenPort: httpsListenPort, serverCmd: serverCmd, serviceCmd: serviceCmd})
		startCommand(serverCmd)
		startCommand(serviceCmd)
	}

	// Start Load balancer
	serverHostsFile := path.Join(configDir, "server_hosts.txt")
	err := ioutil.WriteFile(serverHostsFile, []byte(serverHosts), 0644)
	if err != nil {
		return fmt.Errorf("Failed to write serverHostsFile: %v", err)
	}
	cc.balancerCmd = exec.Command("fleetspeak/src/e2etesting/balancer/balancer", "--servers_file", serverHostsFile, "--frontend_address", frontendAddress)
	startCommand(cc.balancerCmd)

	// Start clients
	serversStartTime := time.Now()
	for i := range numClients {
		linuxConfigPath := path.Join(configDir, fmt.Sprintf("linux_client%v.config", i))
		stateFilePath := path.Join(configDir, fmt.Sprintf("client%v.state", i))
		err := modifyFleetspeakClientConfig(configDir, frontendAddress, linuxConfigPath, stateFilePath, configDir)
		if err != nil {
			return fmt.Errorf("Failed to build FS client configurations: %v", err)
		}
		cmd := exec.Command("fleetspeak/src/client/client/client", "-logtostderr", "-config", linuxConfigPath)
		cc.clientCmds = append(cc.clientCmds, cmd)
		startCommand(cmd)
	}

	newIDs, err := WaitForNewClientIDs(fmt.Sprintf("localhost:%v", firstAdminPort), serversStartTime, numClients)
	if err != nil {
		return fmt.Errorf("Error in waiting for clients: %v", err)
	}
	cc.ClientIDs = newIDs
	return nil
}

// ConfigureAndStart configures and starts fleetspeak servers, clients, their services and FRR master server
func (cc *ComponentsInfo) ConfigureAndStart(mysqlCredentials MysqlCredentials, frontendAddress, msAddress string, numServers, numClients int) error {
	configDir, err := ioutil.TempDir(os.TempDir(), "*_fleetspeak")
	if err != nil {
		return fmt.Errorf("Failed to create temporary dir: %v", err)
	}

	err = buildBaseConfiguration(configDir, mysqlCredentials, frontendAddress)
	if err != nil {
		return fmt.Errorf("Failed to build base Fleetspeak configuration: %v", err)
	}

	err = cc.start(configDir, frontendAddress, msAddress, numServers, numClients)
	if err != nil {
		cc.KillAll()
		return err
	}
	return nil
}
