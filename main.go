package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var timeoutSecond = 600
var ctx = context.Background()
var RABBITMQ_ERLANG_COOKIE = "THISISERLANGCOOKIE"

func createSite(siteName string, rabbitmqConfigFilename string, timeoutSecond int, nodeNames []string) {
	siteNetworkName := siteName + "-net"
	createNetwork(siteNetworkName)
	//removeNetworkFunc := createNetwork(siteNetworkName)
	//defer removeNetworkFunc()

	for _, nodeName := range nodeNames {
		go func(nodeName string) {
			container := createRabbitMQNode(nodeName, siteNetworkName, rabbitmqConfigFilename, timeoutSecond)
			printRabbitMQMappedPorts(container)
			printContainerNetwork(container)
		}(nodeName)
		//defer container.Terminate(ctx)

	}
}

func main() {

	createSite("site1", "rabbitmq-site1.conf", timeoutSecond, []string{"node1", "node2"})
	createSite("site2", "rabbitmq-site2.conf", timeoutSecond, []string{"node3", "node4"})

	var c = make(chan struct{})
	fmt.Println(<-c)
}

func createNetwork(networkName string) func() {
	network, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name:           networkName,
			CheckDuplicate: true,
			SkipReaper:     true,
		},
	})
	if err == nil {
		//New network
		//time.Sleep(time.Second * 2) //very bad way to fix the "network yet create but container still able to start issue
		for try := 0; ; try++ {
			cmd := exec.Command("docker", "network", "inspect", networkName)
			cmd.Stdout = os.Stdout
			err := cmd.Run()
			if err == nil {
				break
			}
			fmt.Println("Sleep 1 second to wait for Docker network to come up, try: ", try)
			if try > 10 {
				fmt.Println("Give up awaiting docker network to come up!")
				panic("timeout waiting docker network to come up")
			}
			time.Sleep(time.Second * 1) //wait for it to success
		}
		//TODO: Find a way to detect if an network create by `docker network create` is success or not
		fmt.Println("Created network:", networkName)
	} else {
		wrappedErr := errors.Unwrap(err)
		if resultError, ok := wrappedErr.(errdefs.ErrConflict); !ok {
			panic(resultError) //we only take action if it's NOT "Network already exists"
		}
	}
	return func() {
		fmt.Println("Removing docker network: ", networkName)
		network.Remove(ctx)
	}
}

func printContainerNetwork(container testcontainers.Container) {
	fmt.Println(container.Host(ctx))
	fmt.Println(container.Name(ctx))
}

func createRabbitMQNode(nodeName string, networkName string, rabbitmqConfigFilename string, timeoutSecond int) testcontainers.Container {
	exposedPorts := GetRabbitMQExposedPorts()
	rabbitmqConfigHostFilePath, err := filepath.Abs(rabbitmqConfigFilename)
	if err != nil {
		panic(err)
	}
	rabbitmqConfigInsidePath := "/rabbitmq.conf"

	rbmqContainer, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{
			Started: true,
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "rabbitmq:3.10.0-management",
				ExposedPorts: exposedPorts, //[]string{"5671", "15692", "4369", "5672", "15691", "25672", "15671-15672", "8080"},
				Resources: container.Resources{
					Memory: 100 * 1024 * 1024, //100MB
				},
				Hostname: nodeName,
				Networks: []string{networkName},
				NetworkAliases: map[string][]string{
					networkName: {nodeName},
				},
				WaitingFor: wait.ForListeningPort("15672"), //.WithStartupTimeout(time.Second * time.Duration(timeoutSecond)),
				Env: map[string]string{ // https://www.rabbitmq.com/configure.html#supported-environment-variables
					"RABBITMQ_ERLANG_COOKIE": RABBITMQ_ERLANG_COOKIE,
					"RABBITMQ_CONFIG_FILE":   rabbitmqConfigInsidePath,
				},
				Mounts: testcontainers.ContainerMounts{
					{
						Source:   testcontainers.GenericBindMountSource{HostPath: rabbitmqConfigHostFilePath},
						Target:   testcontainers.ContainerMountTarget(rabbitmqConfigInsidePath),
						ReadOnly: false,
					},
				},
			},
		},
	)
	if err != nil {
		panic(err)
	}
	rbmqContainer.Exec(ctx, []string{"apt", "update"})
	rbmqContainer.Exec(ctx, []string{"apt", "install", "-yq", "dnsutils", "iputils-ping", "iproute2"})
	return rbmqContainer
}

func printRabbitMQMappedPorts(container testcontainers.Container) {
	for portName, portRange := range rabbitmqPorts {
		port, err := container.MappedPort(ctx, nat.Port(portRange))
		if err != nil {
			continue
		}
		fmt.Printf("%s : %s\n", portName, port)
	}
}

func GetRabbitMQExposedPorts() []string {
	ports := make([]string, 0)
	for _, portRange := range rabbitmqPorts {
		splitted := strings.Split(portRange, "-")
		switch len(splitted) {
		case 1:
			ports = append(ports, splitted[0])
		case 2: //it's a range!
			begin, err := strconv.Atoi(splitted[0])
			if err != nil {
				panic(err)
			}
			end, err := strconv.Atoi(splitted[1])
			if err != nil {
				panic(err)
			}
			for i := begin; i <= end; i++ {
				ports = append(ports, strconv.Itoa(i))
			}
		}
	}
	return ports
}

var rabbitmqPorts = map[string]string{
	//"epmd":                              "4369",
	"AMQP 0-9-1 and AMQP 1.0 - Plain":   "5672",
	"AMQP 0-9-1 and AMQP 1.0 - TLS":     "5671",
	"RabbitMQ Stream protocol - Plain ": "5552",
	"RabbitMQ Stream protocol - TLS":    "5551",
	//"RabbitMQ Stream replication":       "6000-6500",
	"Erlang distribution server port - inter-node and CLI tools communication (AMQP port + 20000)": "25672",
	"Erlang distribution client port - CLI tools":                                                  "35672-35682",
	"Management UI + HTTP API - Plain":                                                             "15672",
	"Management UI + HTTP API - TLS":                                                               "15671",
	//"STOMP clients - Plain":                                                                        "61613",
	//"STOMP clients - TLS":                                                                          "61614",
	//"MQTT clients - TLS":                                                                           "8883",
	//"MQTT clients - Plain":                                                                         "1883",
	//"STOMP-over-WebSockets":                                                                        "15674",
	//"MQTT-over-WebSockets":                                                                         "15675",
	"Prometheus": "15692",
}
