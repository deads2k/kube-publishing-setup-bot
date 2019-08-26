package makepicklist

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/openshift/kube-publishing-setup-bot/pkg/genericclioptions"
	"github.com/openshift/kube-publishing-setup-bot/pkg/kubefork"
	"github.com/spf13/cobra"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

type MakePickListOptions struct {
	Streams genericclioptions.IOStreams

	KubeHome string
	OutFile  string

	Repo                string // like kubernetes, api, apimachinery, etc
	ForkOwner           string // like origin
	ForkVersion         string // like 4.2
	KubeVersion         string // like 1.15.0
	PreviousForkVersion string // like 4.1
	PreviousKubeVersion string // like 1.14.3
}

func NewCreateKubeBranchesForOriginOptions(streams genericclioptions.IOStreams) *MakePickListOptions {
	return &MakePickListOptions{
		Streams:  streams,
		KubeHome: "kube-publishing-setup-bot.local/src/k8s.io",
	}
}

// NewCmdCreateClusterQuota is a macro command to create a new cluster quota.
func NewCmdCreateKubeBranchesForOriginOptions(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewCreateKubeBranchesForOriginOptions(streams)
	cmd := &cobra.Command{
		Use: "create-kube-branches-for-origin-options --kube-home=/path/to/k8s.io --origin-version=4.3 --kube-version=1.14.0",
		Long: `
--kube-home must point to /path/to/k8s.io where /path/to/k8s.io/{kubernetes,api,apimachinery,etcd} should be.

This command will auto-create it if necessary and auto-create the repos inside of it.
 1. upstream will be the remote for k8s - git@github.com:/kubernetes/<repo>.git
 2. openshfit will be the remove for openshift forks - git@github.com:/openshift/kubernetes-<repo>.git
`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := o.Run(); err != nil {
				panic(err)
			}
		},
	}

	cmd.Flags().StringVar(&o.KubeHome, "kube-home", o.KubeHome, "points to /path/to/k8s.io where /path/to/k8s.io/{kubernetes,api,apimachinery,etcd} should be.")
	cmd.Flags().StringVar(&o.Repo, "repo", o.ForkVersion, "like kubernetes, apimachinery, client-go")
	cmd.Flags().StringVar(&o.ForkOwner, "fork-owner", o.ForkOwner, "like origin, sdn, oc")
	cmd.Flags().StringVar(&o.ForkVersion, "fork-version", o.ForkVersion, "fork version, like 4.2")
	cmd.Flags().StringVar(&o.KubeVersion, "kube-version", o.KubeVersion, "kube version, like 1.14.1")
	cmd.Flags().StringVar(&o.PreviousForkVersion, "previous-fork-version", o.PreviousForkVersion, "previous fork version to pull a picklist from, like 4.1, 4.2")
	cmd.Flags().StringVar(&o.PreviousKubeVersion, "previous-kube-version", o.PreviousKubeVersion, "previous kube version to pull a picklist from, like 1.14.1, 1.15.0")
	cmd.Flags().StringVar(&o.OutFile, "out-file", o.OutFile, "csv file to write to")

	return cmd
}

func (o *MakePickListOptions) Run() error {
	if len(o.Repo) == 0 {
		return fmt.Errorf("must have repo")
	}
	if len(o.ForkOwner) == 0 {
		return fmt.Errorf("must have fork-owner")
	}
	if len(o.ForkVersion) == 0 {
		return fmt.Errorf("must have fork-version")
	}
	if len(o.KubeVersion) == 0 {
		return fmt.Errorf("must have kube-version")
	}
	if len(o.PreviousForkVersion) == 0 {
		return fmt.Errorf("must have previous-fork-version")
	}
	if len(o.PreviousKubeVersion) == 0 {
		return fmt.Errorf("must have previous-kube-version")
	}
	if len(o.OutFile) == 0 {
		return fmt.Errorf("must have out-file")
	}

	repoInfos, err := kubefork.GetAllKubeRepos(o.Streams, o.KubeHome)
	if err != nil {
		return err
	}

	for i := range repoInfos {
		currInfo := repoInfos[i]
		if currInfo.UpstreamName != o.Repo {
			continue
		}
		fmt.Fprintf(o.Streams.Out, "Check kubernetes/%v\n", currInfo.UpstreamName)

		if err := kubefork.CloneRepo(o.Streams.Indent(), currInfo); err != nil {
			return err
		}
		_, _, err := kubefork.FetchUpdates(o.Streams.Indent(), currInfo)
		if err != nil {
			return err
		}

		repo, err := git.PlainOpen(currInfo.Path)
		if err != nil {
			return err
		}
		if err := o.pickList(o.Streams.Indent(), repo, currInfo.Path); err != nil {
			return err
		}
	}

	return nil
}

func (o *MakePickListOptions) pickList(streams genericclioptions.IOStreams, repo *git.Repository, repoPath string) error {
	prevStartingTag := kubefork.UpstreamTag(o.Repo, o.PreviousKubeVersion)
	prevBranch := kubefork.NewForkBranch(o.ForkOwner, o.PreviousForkVersion, o.PreviousKubeVersion).BranchName()
	//startingTag := kubefork.UpstreamTag(o.Repo, o.KubeVersion)
	//destBranch := kubefork.NewForkBranch(o.ForkOwner, o.ForkVersion, o.KubeVersion).BranchName()

	commits, err := kubefork.CollectCmdStdout(repoPath, "git", "rev-list", prevStartingTag+".."+prevBranch, "--no-merges", "--reverse")
	if err != nil {
		return err
	}

	outfile, err := os.Create(o.OutFile)
	if err != nil {
		return err
	}

	csvWriter := csv.NewWriter(outfile)
	csvWriter.Write([]string{"description", "fork-commit", "upstream-commit", "upstream-on-release-commit"})

	for _, commit := range strings.Split(commits, "\n") {
		if len(commit) == 0 {
			continue
		}

		record := []string{}

		commitUncastObj, err := repo.Object(plumbing.CommitObject, plumbing.NewHash(commit))
		if err != nil {
			return err
		}
		commitObj := commitUncastObj.(*object.Commit)
		record = append(record, strings.Split(commitObj.Message, "\n")[0])

		record = append(record, commit)

		if err := csvWriter.Write(record); err != nil {
			return err
		}
		csvWriter.Flush()
	}

	outfile.Close()

	return nil
}
