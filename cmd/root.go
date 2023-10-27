package cmd

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cloudbees-io/configure-ecr-credentials/internal/auth"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cmd = &cobra.Command{
		Use:          "configure-ecr-credentials",
		Short:        "Configures credentials for accessing ECR",
		Long:         "Configures credentials for accessing ECR",
		SilenceUsage: true,
		RunE:         doConfigure,
	}
)

func init() {
	viper.AutomaticEnv()

	viper.SetEnvPrefix("INPUT")

	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)

	inputString("registries", "", "A comma-delimited list of AWS account IDs that are associated with the ECR Private registries")
	inputString("registry-type", "", "Which ECR registry type to log into")
}

func inputString(name string, value string, usage string) {
	cmd.Flags().String(name, value, usage)
	_ = viper.BindPFlag(name, cmd.Flags().Lookup(name))
}

func Execute() error {
	return cmd.Execute()
}

func cliContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel() // exit gracefully
		<-c
		os.Exit(1) // exit immediately on 2nd signal
	}()
	return ctx
}

func doConfigure(command *cobra.Command, args []string) error {
	ctx := cliContext()

	var cfg auth.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return err
	}

	return cfg.Authenticate(ctx)
}
