package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"softrains-detector/pkg/softrainsrpc"
)

func main() {
	endpoint := flag.String("endpoint", "192.168.68.61:8443", "relay endpoint host:port")
	serverName := flag.String("server-name", "softrains.local", "TLS server name")
	caCert := flag.String("ca-cert", "", "optional CA cert PEM path")
	clientCert := flag.String("client-cert", "", "optional client cert PEM path (required for mTLS)")
	clientKey := flag.String("client-key", "", "optional client key PEM path (required for mTLS)")
	insecureSkipVerify := flag.Bool("insecure-skip-verify", false, "skip TLS certificate/hostname verification (testing only)")
	timeout := flag.Duration("timeout", 15*time.Second, "request timeout (Pico TLS handshake may be slow)")

	action := flag.String("action", "ping", "action: ping|getall|get|set")
	relay := flag.Int("relay", 0, "relay index for get/set")
	state := flag.String("state", "off", "target relay state for set: on|off")
	requestID := flag.String("request-id", "relaytester", "request correlation id")

	flag.Parse()

	var (
		client *softrainsrpc.RelayClient
		err    error
	)

	if *insecureSkipVerify {
		tlsCfg := &tls.Config{
			MinVersion:         tls.VersionTLS12,
			MaxVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true,
			ServerName:         *serverName,
		}
		client, err = softrainsrpc.NewRelayClientWithTLSConfig(*endpoint, tlsCfg, *timeout)
	} else if *clientCert != "" || *clientKey != "" {
		client, err = softrainsrpc.NewRelayClientMTLS(*endpoint, *serverName, *caCert, *clientCert, *clientKey, *timeout)
	} else {
		client, err = softrainsrpc.NewRelayClient(*endpoint, *serverName, *caCert, *timeout)
	}
	if err != nil {
		fmt.Printf("failed to create relay client: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch *action {
	case "ping":
		env, err := client.Ping(ctx, *requestID)
		exitOnErr(err)
		printJSON(env)
	case "getall":
		states, env, err := client.GetAllStates(ctx, *requestID)
		exitOnErr(err)
		printJSON(env)
		printJSON(states)
	case "get":
		result, env, err := client.GetRelay(ctx, *relay, *requestID)
		exitOnErr(err)
		printJSON(env)
		printJSON(result)
	case "set":
		env, err := client.SetRelay(ctx, *relay, *state, *requestID)
		exitOnErr(err)
		printJSON(env)
		result, env2, err := client.GetRelay(ctx, *relay, *requestID+"-verify")
		exitOnErr(err)
		printJSON(env2)
		printJSON(result)
	default:
		fmt.Printf("unknown action: %s\n", *action)
		os.Exit(2)
	}
}

func printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("marshal error: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

func exitOnErr(err error) {
	if err == nil {
		return
	}
	fmt.Printf("request failed: %v\n", err)
	os.Exit(1)
}
