package main

import (
	"context"
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

type startedProcessesPids struct {
	serverPid  chan int
	clientPid  chan int
	servicePid chan int
}

func (sp *startedProcessesPids) init() {
	sp.serverPid = make(chan int, 1)
	sp.clientPid = make(chan int, 1)
	sp.servicePid = make(chan int, 1)
}

func killProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	process.Kill()
}

func (sp *startedProcessesPids) killAll() {
	close(sp.serverPid)
	close(sp.clientPid)
	close(sp.servicePid)
	for pid := range sp.serverPid {
		killProcess(pid)
	}
	for pid := range sp.clientPid {
		killProcess(pid)
	}
	for pid := range sp.servicePid {
		killProcess(pid)
	}
}

// Starts a command and redirects its output to main stdout
func startProcess(cmd *exec.Cmd, pidChan chan int) {
	CopyAndCapture := func(w io.Writer, r io.Reader) ([]byte, error) {
		var out []byte
		buf := make([]byte, 1024, 1024)
		for {
			n, err := r.Read(buf[:])
			if n > 0 {
				d := buf[:n]
				out = append(out, d...)
				_, err := w.Write(d)
				if err != nil {
					return out, err
				}
			}
			if err != nil {
				// Read returns io.EOF at the end of file, which is not an error for us
				if err == io.EOF {
					err = nil
				}
				return out, err
			}
		}
	}

	stdoutIn, _ := cmd.StdoutPipe()
	stderrIn, _ := cmd.StderrPipe()
	err := cmd.Start()
	if err != nil {
		fmt.Println("cmd.Start() failed: " + err.Error())
	}
	pidChan <- cmd.Process.Pid

	// cmd.Wait() should be called only after we finish reading
	// from stdoutIn and stderrIn.
	// wg ensures that we finish
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		CopyAndCapture(os.Stdout, stdoutIn)
		wg.Done()
	}()

	CopyAndCapture(os.Stderr, stderrIn)
	wg.Wait()
	cmd.Wait()
}

func getNewClientID(adminPort int, startTime time.Time) string {
	adminAddr := fmt.Sprintf("localhost:%v", adminPort)
	conn, err := grpc.Dial(adminAddr, grpc.WithInsecure())
	if err != nil {
		fmt.Printf("Unable to connect to fleetspeak admin interface [%v]: %v", adminAddr, err)
	}
	admin := servicesPb.NewAdminClient(conn)
	ctx := context.Background()
	var ids [][]byte
	for i := 1; i <= 10; i++ {
		if i > 1 {
			time.Sleep(time.Second * 1)
		}
		res, err := admin.ListClients(ctx,
			&servicesPb.ListClientsRequest{ClientIds: ids},
			grpc.MaxCallRecvMsgSize(1024*1024*1024))
		if err != nil {
			fmt.Printf("ListClients RPC failed: %v", err)
			continue
		}
		if len(res.Clients) == 0 {
			fmt.Printf("No new clients yet")
			continue
		}
		client := res.Clients[0]
		lastVisitTime, _ := ptypes.Timestamp(client.LastContactTime)
		for _, cl := range res.Clients {
			curLastVisitTime, _ := ptypes.Timestamp(cl.LastContactTime)
			if curLastVisitTime.After(lastVisitTime) {
				client = cl
				lastVisitTime = curLastVisitTime
			}
		}

		id, err := common.BytesToClientID(client.ClientId)
		if err != nil {
			fmt.Printf("Ignoring invalid client id [%v], %v", client.ClientId, err)
			continue
		}
		ts, err := ptypes.Timestamp(client.LastContactTime)
		if ts.After(startTime) {
			return fmt.Sprintf("%v", id)
		}
	}
	return ""
}

func configureFleetspeak(tempPath string, fsFrontendPort, fsAdminPort int) {
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
	ioutil.WriteFile(builtConfiguratorConfigPath, []byte(proto.MarshalTextString(&config)), 0644)

	// Build fleetspeak configurations
	_, err := exec.Command("config", "-config", builtConfiguratorConfigPath).Output()
	if err != nil {
		fmt.Println("error: " + err.Error())
	}

	// Adjust client config
	var clientConfig clientConfigPb.Config
	clientConfigData, err := ioutil.ReadFile(config.LinuxClientConfigurationFile)
	proto.UnmarshalText(string(clientConfigData), &clientConfig)
	clientConfig.GetFilesystemHandler().ConfigurationDirectory = tempPath
	clientConfig.GetFilesystemHandler().StateFile = path.Join(tempPath, "client.state")
	os.Create(clientConfig.GetFilesystemHandler().StateFile)
	ioutil.WriteFile(config.LinuxClientConfigurationFile, []byte(proto.MarshalTextString(&clientConfig)), 0644)

	// Client services configuration
	clientServiceConf := spb.ClientServiceConfig{Name: "FRR_client", Factory: "Daemon"}
	var payload daemonservicePb.Config
	payload.Argv = append(payload.Argv, "python", "../../../../frr_python/frr_client.py")
	clientServiceConf.Config, _ = ptypes.MarshalAny(&payload)
	os.Mkdir(path.Join(tempPath, "textservices"), 0777)
	os.Mkdir(path.Join(tempPath, "services"), 0777)
	os.Create(path.Join(tempPath, "communicator.txt"))
	ioutil.WriteFile(path.Join(tempPath, "textservices", "frr.textproto"), []byte(proto.MarshalTextString(&clientServiceConf)), 0644)

	// Server services configuration
	serverServiceConf := servicesPb.ServiceConfig{Name: "FRR_server", Factory: "GRPC"}
	grpcConfig := grpcServicePb.Config{Target: fmt.Sprintf("localhost:%v", fsFrontendPort), Insecure: true}
	serverServiceConf.Config, _ = ptypes.MarshalAny(&grpcConfig)
	serverConf := servicesPb.ServerConfig{Services: []*servicesPb.ServiceConfig{&serverServiceConf}, BroadcastPollTime: new(duration.Duration)}
	serverConf.BroadcastPollTime.Seconds = 1
	builtServerServicesConfigPath := path.Join(tempPath, "server.services.config")
	ioutil.WriteFile(builtServerServicesConfigPath, []byte(proto.MarshalTextString(&serverConf)), 0644)
}

func startProcesses(startedProcesses *startedProcessesPids, tempPath string, fsFrontendPort, fsAdminPort int) {
	// Start server
	serverRunCmd := exec.Command("server", "-logtostderr", "-components_config", path.Join(tempPath, "server.config"), "-services_config", path.Join(tempPath, "server.services.config"))
	go startProcess(serverRunCmd, startedProcesses.serverPid)

	serverStartTime := time.Now()

	// Sleep and start client
	time.Sleep(time.Second * 1)
	clientRunCmd := exec.Command("client", "-logtostderr", "-config", path.Join(tempPath, "linux_client.config"))
	go startProcess(clientRunCmd, startedProcesses.clientPid)

	// Get new client's id and start service in current process
	clientID := getNewClientID(fsAdminPort, serverStartTime)
	fmt.Println("Enrolled client_id: ", clientID)
	serviceRunCmd := exec.Command(
		"python",
		"../../../../frr_python/frr_server.py",
		fmt.Sprintf("--client_id=%v", clientID),
		fmt.Sprintf("--fleetspeak_message_listen_address=localhost:%v", fsFrontendPort),
		fmt.Sprintf("--fleetspeak_server=localhost:%v", fsAdminPort))
	startProcess(serviceRunCmd, startedProcesses.servicePid)
}

func main() {
	flag.Parse()

	tempPath, _ := ioutil.TempDir(os.TempDir(), "*_fleetspeak")
	fsFrontendPort := 6062
	fsAdminPort := 6061

	configureFleetspeak(tempPath, fsFrontendPort, fsAdminPort)

	var startedProcesses startedProcessesPids
	startedProcesses.init()
	defer startedProcesses.killAll()

	startProcesses(&startedProcesses, tempPath, fsFrontendPort, fsAdminPort)
}
