package setup

import (
	"context"
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	ptypes "github.com/golang/protobuf/ptypes"
	duration "github.com/golang/protobuf/ptypes/duration"
	daemonservicePb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	clientConfigPb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	spb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
	fcpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	grpcServicePb "github.com/google/fleetspeak/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice"
	servicesPb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	"google.golang.org/grpc"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"time"
)

type serverInfo struct {
	serverCmd       *exec.Cmd
	serviceCmd      *exec.Cmd
	httpsListenPort int
}

// ComponentsInfo contains IDs of connected clients and exec.Cmds for all components
type ComponentsInfo struct {
	masterServerCmd *exec.Cmd
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

func getNewClientIDs(admin servicesPb.AdminClient, startTime time.Time) ([]string, error) {
	var ids [][]byte
	ctx := context.Background()
	res, err := admin.ListClients(ctx,
		&servicesPb.ListClientsRequest{ClientIds: ids},
		grpc.MaxCallRecvMsgSize(1024*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("ListClients RPC failed: %v", err)
	}

	var newClients []string
	for _, cl := range res.Clients {
		lastContactTime, err := ptypes.Timestamp(cl.LastContactTime)
		if err != nil {
			continue
		}
		if lastContactTime.After(startTime) {
			id, err := common.BytesToClientID(cl.ClientId)
			if err == nil {
				newClients = append(newClients, fmt.Sprintf("%v", id))
			}
		}
	}
	return newClients, nil
}

func waitForNewClientID(adminPort int, startTime time.Time, numClients int) ([]string, error) {
	adminAddr := fmt.Sprintf("localhost:%v", adminPort)
	conn, err := grpc.Dial(adminAddr, grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to fleetspeak admin interface [%v]: %v", adminAddr, err)
	}
	admin := servicesPb.NewAdminClient(conn)
	for i := 0; i < 10; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		ids, err := getNewClientIDs(admin, startTime)
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
	return nil, errors.New("Not all clients connected")
}

// MysqlCredentials contains username, password and database
type MysqlCredentials struct {
	Username string
	Password string
	Database string
}

func buildBaseConfiguration(tempPath string, mysqlCredentials MysqlCredentials) error {
	var config cpb.Config
	config.ConfigurationName = "FleetspeakSetup"

	config.ComponentsConfig = new(fcpb.Config)
	config.ComponentsConfig.MysqlDataSourceName =
		fmt.Sprintf("%v:%v@tcp(127.0.0.1:3306)/%v", mysqlCredentials.Username, mysqlCredentials.Password, mysqlCredentials.Database)

	config.ComponentsConfig.HttpsConfig = new(fcpb.HttpsConfig)
	config.ComponentsConfig.HttpsConfig.ListenAddress = fmt.Sprintf("localhost:6060")
	config.ComponentsConfig.HttpsConfig.DisableStreaming = false

	config.ComponentsConfig.AdminConfig = new(fcpb.AdminConfig)
	config.ComponentsConfig.AdminConfig.ListenAddress = fmt.Sprintf("localhost:6061")

	config.PublicHostPort =
		append(config.PublicHostPort, config.ComponentsConfig.HttpsConfig.ListenAddress)

	config.ServerComponentConfigurationFile = path.Join(tempPath, "server.config")
	config.TrustedCertFile = path.Join(tempPath, "trusted_cert.pem")
	config.TrustedCertKeyFile = path.Join(tempPath, "trusted_cert_key.pem")
	config.ServerCertFile = path.Join(tempPath, "server_cert.pem")
	config.ServerCertKeyFile = path.Join(tempPath, "server_cert_key.pem")
	config.LinuxClientConfigurationFile = path.Join(tempPath, "linux_client.config")
	config.WindowsClientConfigurationFile = path.Join(tempPath, "windows_client.config")
	config.DarwinClientConfigurationFile = path.Join(tempPath, "darwin_client.config")

	builtConfiguratorConfigPath := path.Join(tempPath, "configurator.config")
	err := ioutil.WriteFile(builtConfiguratorConfigPath, []byte(proto.MarshalTextString(&config)), 0644)
	if err != nil {
		return fmt.Errorf("Unable to write configurator file: %v", err)
	}

	// Build fleetspeak configurations
	_, err = exec.Command("config", "-config", builtConfiguratorConfigPath).Output()
	if err != nil {
		return fmt.Errorf("Failed to build Fleetspeak configurations: %v", err)
	}

	// Client services configuration
	clientServiceConfig := spb.ClientServiceConfig{Name: "FRR", Factory: "Daemon"}
	var payload daemonservicePb.Config
	payload.Argv = append(payload.Argv, "python", "frr_python/frr_client.py")
	clientServiceConfig.Config, err = ptypes.MarshalAny(&payload)
	if err != nil {
		return fmt.Errorf("Failed to marshal client service configuration: %v", err)
	}
	err = os.Mkdir(path.Join(tempPath, "textservices"), 0777)
	if err != nil {
		return fmt.Errorf("Unable to create textservices directory: %v", err)
	}
	err = os.Mkdir(path.Join(tempPath, "services"), 0777)
	if err != nil {
		return fmt.Errorf("Unable to create services directory: %v", err)
	}
	_, err = os.Create(path.Join(tempPath, "communicator.txt"))
	if err != nil {
		return fmt.Errorf("Unable to create communicator.txt: %v", err)
	}
	err = ioutil.WriteFile(path.Join(tempPath, "textservices", "frr.textproto"), []byte(proto.MarshalTextString(&clientServiceConfig)), 0644)
	if err != nil {
		return fmt.Errorf("Unable to write frr.textproto file: %v", err)
	}
	return nil
}

func configureFleetspeakServer(tempPath string, fsFrontendPort, fsHTTPSListenPort, fsAdminPort int, serverConfigPath, serverServicesConfigPath string) error {
	// Update server addresses
	serverBaseConfigurationPath := path.Join(tempPath, "server.config")

	var serverConfig fcpb.Config
	serverConfigData, err := ioutil.ReadFile(serverBaseConfigurationPath)
	if err != nil {
		return fmt.Errorf("Unable to read server.config: %v", err)
	}
	err = proto.UnmarshalText(string(serverConfigData), &serverConfig)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal server.config: %v", err)
	}
	serverConfig.HttpsConfig.ListenAddress = fmt.Sprintf("localhost:%v", fsHTTPSListenPort)
	serverConfig.AdminConfig.ListenAddress = fmt.Sprintf("localhost:%v", fsAdminPort)
	err = ioutil.WriteFile(serverConfigPath, []byte(proto.MarshalTextString(&serverConfig)), 0644)

	// Server services configuration
	serverServiceConf := servicesPb.ServiceConfig{Name: "FRR", Factory: "GRPC"}
	grpcConfig := grpcServicePb.Config{Target: fmt.Sprintf("localhost:%v", fsFrontendPort), Insecure: true}
	serviceConfig, err := ptypes.MarshalAny(&grpcConfig)
	if err != nil {
		return fmt.Errorf("Failed to marshal grpcConfig: %v", err)
	}
	serverServiceConf.Config = serviceConfig
	serverConf := servicesPb.ServerConfig{Services: []*servicesPb.ServiceConfig{&serverServiceConf}, BroadcastPollTime: &duration.Duration{Seconds: 1}}
	err = ioutil.WriteFile(serverServicesConfigPath, []byte(proto.MarshalTextString(&serverConf)), 0644)
	if err != nil {
		return fmt.Errorf("Unable to write server.services.config: %v", err)
	}
	return nil
}

func configureFleetspeakClient(tempPath string, httpsListenPort int, linuxConfigPath, stateFilePath string) error {
	linuxBaseConfigPath := path.Join(tempPath, "linux_client.config")

	var clientConfig clientConfigPb.Config
	clientConfigData, err := ioutil.ReadFile(linuxBaseConfigPath)
	if err != nil {
		return fmt.Errorf("Unable to read LinuxClientConfigurationFile: %v", err)
	}
	err = proto.UnmarshalText(string(clientConfigData), &clientConfig)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal clientConfigData: %v", err)
	}
	clientConfig.GetFilesystemHandler().ConfigurationDirectory = tempPath
	clientConfig.GetFilesystemHandler().StateFile = stateFilePath
	clientConfig.Server = []string{fmt.Sprintf("localhost:%v", httpsListenPort)}
	_, err = os.Create(clientConfig.GetFilesystemHandler().StateFile)
	if err != nil {
		return fmt.Errorf("Failed to create client state file: %v", err)
	}
	err = ioutil.WriteFile(linuxConfigPath, []byte(proto.MarshalTextString(&clientConfig)), 0644)
	if err != nil {
		return fmt.Errorf("Failed to update LinuxClientConfigurationFile: %v", err)
	}

	return nil
}

func (cc *ComponentsInfo) start(tempPath string, msPort int, numServers, numClients int) error {
	firstAdminPort := 6060

	// Start Master server
	cc.masterServerCmd = exec.Command("fleetspeak/src/e2etesting/frr-master-server-main/frr_master_server_main", "--listen_address", fmt.Sprintf("localhost:%v", msPort), "--admin_address", fmt.Sprintf("localhost:%v", firstAdminPort))
	startCommand(cc.masterServerCmd)

	// Start servers and their services
	for i := 0; i < numServers; i++ {
		adminPort := firstAdminPort + i*3
		httpsListenPort := adminPort + 1
		frontendPort := adminPort + 2
		cc.servers = append(cc.servers, serverInfo{httpsListenPort: httpsListenPort})
		serverConfigPath := path.Join(tempPath, fmt.Sprintf("server%v.config", i))
		serverServicesConfigPath := path.Join(tempPath, fmt.Sprintf("server%v.services.config", i))
		err := configureFleetspeakServer(tempPath, frontendPort, httpsListenPort, adminPort, serverConfigPath, serverServicesConfigPath)
		if err != nil {
			return fmt.Errorf("Failed to build FS server configurations: %v", err)
		}
		cc.servers[len(cc.servers)-1].serverCmd = exec.Command("server", "-logtostderr", "-components_config", serverConfigPath, "-services_config", serverServicesConfigPath)
		startCommand(cc.servers[len(cc.servers)-1].serverCmd)

		cc.servers[len(cc.servers)-1].serviceCmd = exec.Command(
			"python",
			"frr_python/frr_server.py",
			fmt.Sprintf("--master_server_address=localhost:%v", msPort),
			fmt.Sprintf("--fleetspeak_message_listen_address=localhost:%v", frontendPort),
			fmt.Sprintf("--fleetspeak_server=localhost:%v", adminPort))
		startCommand(cc.servers[len(cc.servers)-1].serviceCmd)
	}

	serversStartTime := time.Now()

	// Start clients
	for i := 0; i < numClients; i++ {
		httpsServerPort := cc.servers[i%numServers].httpsListenPort
		linuxConfigPath := path.Join(tempPath, fmt.Sprintf("linux_client%v.config", i))
		stateFilePath := path.Join(tempPath, fmt.Sprintf("client%v.state", i))
		err := configureFleetspeakClient(tempPath, httpsServerPort, linuxConfigPath, stateFilePath)
		if err != nil {
			return fmt.Errorf("Failed to build FS client configurations: %v", err)
		}
		cc.clientCmds = append(cc.clientCmds, exec.Command("client", "-logtostderr", "-config", linuxConfigPath))
		startCommand(cc.clientCmds[len(cc.clientCmds)-1])
	}

	newIDs, err := waitForNewClientID(firstAdminPort, serversStartTime, numClients)
	if err != nil {
		return fmt.Errorf("Error in waiting for clients: %v", err)
	}
	cc.ClientIDs = newIDs
	return nil
}

// ConfigureAndStart configures and starts fleetspeak servers, clients, their services and FRR master server
func (cc *ComponentsInfo) ConfigureAndStart(mysqlCredentials MysqlCredentials, msPort int, numServers, numClients int) error {
	tempPath, err := ioutil.TempDir(os.TempDir(), "*_fleetspeak")
	if err != nil {
		return fmt.Errorf("Failed to create temporary dir: %v", err)
	}

	err = buildBaseConfiguration(tempPath, mysqlCredentials)
	if err != nil {
		return fmt.Errorf("Failed to build base Fleetspeak configuration: %v", err)
	}

	err = cc.start(tempPath, msPort, numServers, numClients)
	if err != nil {
		cc.KillAll()
		return err
	}
	return nil
}
