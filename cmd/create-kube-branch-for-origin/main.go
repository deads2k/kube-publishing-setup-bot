package main

import (
	"math/rand"
	"os"
	"time"

	"github.com/openshift/kube-publishing-setup-bot/pkg/createkubebranchesfororigin"
	"github.com/openshift/kube-publishing-setup-bot/pkg/genericclioptions"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	command := createkubebranchesfororigin.NewCmdCreateKubeBranchesForOriginOptions(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
