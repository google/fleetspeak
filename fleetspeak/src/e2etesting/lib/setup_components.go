package main

import (
	"context"
	"errors"
	"flag"
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
	"sync"
	"time"
)

var (
	mysqlDatabase = flag.String("mysql_database", "", "MySQL database name to use")
	mysqlUsername = flag.String("mysql_username", "", "MySQL username to use")
	mysqlPassword = flag.String("mysql_password", "", "MySQL password to use")
)

type componentCmds struct {
	serverCmd  *exec.Cmd
	clientCmd  *exec.Cmd
	serviceCmd *exec.Cmd
}

func (cc *componentCmds) killAll() {
	if cc.serverCmd != nil && cc.serverCmd.Process != nil {
		cc.serverCmd.Process.Kill()
	}
	if cc.clientCmd != nil && cc.clientCmd.Process != nil {
		cc.clientCmd.Process.Kill()
	}
}

// Runs a command and redirects its output to main stdout
func runCommand(cmd *exec.Cmd) error {
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

	// cmd.Wait() should be called only after we finish reading
	// from stdoutIn and stderrIn.
	// wg ensures that we finish
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		io.Copy(os.Stdout, stdoutIn)
		wg.Done()
	}()

	_, err = io.Copy(os.Stderr, stderrIn)
	if err != nil {
		return fmt.Errorf("Copy streams error: %v", err)
	}

	wg.Wait()
	cmd.Wait()
	return nil
}

func getNewClientID(admin servicesPb.AdminClient, startTime time.Time) (string, error) {
	var ids [][]byte
	ctx := context.Background()
	res, err := admin.ListClients(ctx,
		&servicesPb.ListClientsRequest{ClientIds: ids},
		grpc.MaxCallRecvMsgSize(1024*1024*1024))
	if err != nil {
		return "", fmt.Errorf("ListClients RPC failed: %v", err)
	}
	if len(res.Clients) == 0 {
		return "", errors.New("No new clients")
	}
	var client *servicesPb.Client
	lastVisitTime := startTime
	for _, cl := range res.Clients {
		curLastVisitTime, err := ptypes.Timestamp(cl.LastContactTime)
		if err != nil {
			continue
		}
		if curLastVisitTime.After(lastVisitTime) {
			client = cl
			lastVisitTime = curLastVisitTime
		}
	}
	if client == nil {
		return "", errors.New("No new clients after startTime")
	}
	id, err := common.BytesToClientID(client.ClientId)
	if err != nil {
		return "", fmt.Errorf("Invalid client id [%v]: %v", client.ClientId, err)
	}
	return fmt.Sprintf("%v", id), nil
}

func waitForNewClientID(adminPort int, startTime time.Time) (string, error) {
	adminAddr := fmt.Sprintf("localhost:%v", adminPort)
	conn, err := grpc.Dial(adminAddr, grpc.WithInsecure())
	if err != nil {
		return "", fmt.Errorf("Failed to connect to fleetspeak admin interface [%v]: %v", adminAddr, err)
	}
	admin := servicesPb.NewAdminClient(conn)
	for i := 0; i < 10; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		id, err := getNewClientID(admin, startTime)
		if err == nil {
			return id, nil
		}
	}
	return "", errors.New("No connected clients")
}

func configureFleetspeak(tempPath string, fsFrontendPort, fsAdminPort int) error {
	var config cpb.Config
	config.ConfigurationName = "FleetspeakSetup"

	config.ComponentsConfig = new(fcpb.Config)
	config.ComponentsConfig.MysqlDataSourceName =
		fmt.Sprintf("%v:%v@tcp(127.0.0.1:3306)/%v", *mysqlUsername, *mysqlPassword, *mysqlDatabase)

	config.ComponentsConfig.HttpsConfig = new(fcpb.HttpsConfig)
	config.ComponentsConfig.HttpsConfig.ListenAddress = fmt.Sprintf("localhost:6060")
	config.ComponentsConfig.HttpsConfig.DisableStreaming = true

	config.ComponentsConfig.AdminConfig = new(fcpb.AdminConfig)
	config.ComponentsConfig.AdminConfig.ListenAddress = fmt.Sprintf("localhost:%v", fsAdminPort)

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

	// Adjust client config
	var clientConfig clientConfigPb.Config
	clientConfigData, err := ioutil.ReadFile(config.LinuxClientConfigurationFile)
	if err != nil {
		return fmt.Errorf("Unable to read LinuxClientConfigurationFile: %v", err)
	}
	err = proto.UnmarshalText(string(clientConfigData), &clientConfig)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal clientConfigData: %v", err)
	}
	clientConfig.GetFilesystemHandler().ConfigurationDirectory = tempPath
	clientConfig.GetFilesystemHandler().StateFile = path.Join(tempPath, "client.state")
	_, err = os.Create(clientConfig.GetFilesystemHandler().StateFile)
	if err != nil {
		return fmt.Errorf("Failed to create client state file: %v", err)
	}
	err = ioutil.WriteFile(config.LinuxClientConfigurationFile, []byte(proto.MarshalTextString(&clientConfig)), 0644)
	if err != nil {
		return fmt.Errorf("Failed to update LinuxClientConfigurationFile: %v", err)
	}

	// Client services configuration
	clientServiceConf := spb.ClientServiceConfig{Name: "FRR_client", Factory: "Daemon"}
	var payload daemonservicePb.Config
	payload.Argv = append(payload.Argv, "python", "frr_python/frr_client.py")
	clientServiceConf.Config, err = ptypes.MarshalAny(&payload)
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
	err = ioutil.WriteFile(path.Join(tempPath, "textservices", "frr.textproto"), []byte(proto.MarshalTextString(&clientServiceConf)), 0644)
	if err != nil {
		return fmt.Errorf("Unable to write frr.textproto file: %v", err)
	}

	// Server services configuration
	serverServiceConf := servicesPb.ServiceConfig{Name: "FRR_server", Factory: "GRPC"}
	grpcConfig := grpcServicePb.Config{Target: fmt.Sprintf("localhost:%v", fsFrontendPort), Insecure: true}
	serverServiceConf.Config, err = ptypes.MarshalAny(&grpcConfig)
	if err != nil {
		return fmt.Errorf("Failed to marshal grpcConfig: %v", err)
	}
	serverConf := servicesPb.ServerConfig{Services: []*servicesPb.ServiceConfig{&serverServiceConf}, BroadcastPollTime: new(duration.Duration)}
	serverConf.BroadcastPollTime.Seconds = 1
	builtServerServicesConfigPath := path.Join(tempPath, "server.services.config")
	err = ioutil.WriteFile(builtServerServicesConfigPath, []byte(proto.MarshalTextString(&serverConf)), 0644)
	if err != nil {
		return fmt.Errorf("Unable to write server.services.config: %v", err)
	}
	return nil
}

func (cc *componentCmds) start(tempPath string, fsFrontendPort, fsAdminPort int) error {
	// Start server
	cc.serverCmd = exec.Command("server", "-logtostderr", "-components_config", path.Join(tempPath, "server.config"), "-services_config", path.Join(tempPath, "server.services.config"))
	go runCommand(cc.serverCmd)

	serverStartTime := time.Now()

	// Start client
	cc.clientCmd = exec.Command("client", "-logtostderr", "-config", path.Join(tempPath, "linux_client.config"))
	go runCommand(cc.clientCmd)

	// Get new client's id and start service in current process
	clientID, err := waitForNewClientID(fsAdminPort, serverStartTime)
	if err != nil {
		return fmt.Errorf("No new clients have connected: %v", err)
	}
	cc.serviceCmd = exec.Command(
		"python",
		"frr_python/frr_server.py",
		fmt.Sprintf("--client_id=%v", clientID),
		fmt.Sprintf("--fleetspeak_message_listen_address=localhost:%v", fsFrontendPort),
		fmt.Sprintf("--fleetspeak_server=localhost:%v", fsAdminPort))
	runCommand(cc.serviceCmd)
	return nil
}

func run() error {
	flag.Parse()

	fsFrontendPort := 6062
	fsAdminPort := 6061
	tempPath, err := ioutil.TempDir(os.TempDir(), "*_fleetspeak")
	if err != nil {
		return fmt.Errorf("Failed to create temporary dir: %v", err)
	}

	err = configureFleetspeak(tempPath, fsFrontendPort, fsAdminPort)
	if err != nil {
		return fmt.Errorf("Failed to configure Fleetspeak: %v", err)
	}

	var cc componentCmds
	defer cc.killAll()
	err = cc.start(tempPath, fsFrontendPort, fsAdminPort)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
