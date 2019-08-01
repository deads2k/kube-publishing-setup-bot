package main

import (
	"math/rand"
	"os"
	"time"

	"github.com/openshift/kube-publishing-setup-bot/pkg/genericclioptions"
	"github.com/openshift/kube-publishing-setup-bot/pkg/synckubetags"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	command := synckubetags.NewCmdSyncTags(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
