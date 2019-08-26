package createkubebranchesfororigin

import (
	"fmt"
	"strings"

	"github.com/openshift/kube-publishing-setup-bot/pkg/genericclioptions"
	"github.com/openshift/kube-publishing-setup-bot/pkg/kubefork"
	"github.com/spf13/cobra"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

type CreateKubeBranchesForOriginOptions struct {
	Streams genericclioptions.IOStreams

	KubeHome    string
	ForkVersion string
	KubeVersion string
}

func NewCreateKubeBranchesForOriginOptions(streams genericclioptions.IOStreams) *CreateKubeBranchesForOriginOptions {
	return &CreateKubeBranchesForOriginOptions{
		Streams:  streams,
		KubeHome: "kube-publishing-setup-bot.local/src/k8s.io",
	}
}

// NewCmdCreateClusterQuota is a macro command to create a new cluster quota.
func NewCmdCreateKubeBranchesForOriginOptions(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewCreateKubeBranchesForOriginOptions(streams)
	cmd := &cobra.Command{
		Use: "create-kube-branches-for-origin-options --kube-home=/path/to/k8s.io --fork-version=4.3 --kube-version=1.14.0",
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
	cmd.Flags().StringVar(&o.ForkVersion, "fork-version", o.ForkVersion, "fork version, like 4.2")
	cmd.Flags().StringVar(&o.KubeVersion, "kube-version", o.KubeVersion, "kube version, like 1.14.1")

	return cmd
}

func (o *CreateKubeBranchesForOriginOptions) Run() error {
	if len(o.ForkVersion) == 0 {
		return fmt.Errorf("must have fork-version")
	}
	if len(o.KubeVersion) == 0 {
		return fmt.Errorf("must have kube-version")
	}

	repoInfos, err := kubefork.GetAllKubeRepos(o.Streams, o.KubeHome)
	if err != nil {
		return err
	}

	for i := range repoInfos {
		currInfo := repoInfos[i]
		fmt.Fprintf(o.Streams.Out, "Check kubernetes/%v\n", currInfo.UpstreamName)

		if err := kubefork.CloneRepo(o.Streams.Indent(), currInfo); err != nil {
			return err
		}
		_, openshiftRemote, err := kubefork.FetchUpdates(o.Streams.Indent(), currInfo)
		if err != nil {
			return err
		}

		repo, err := git.PlainOpen(currInfo.Path)
		if err != nil {
			return err
		}
		if err := pushOriginForkBranches(o.Streams.Indent(), repo, currInfo.Path, currInfo.UpstreamName, o.KubeVersion, o.ForkVersion, openshiftRemote.Config()); err != nil {
			return err
		}
	}

	return nil
}

func pushOriginForkBranches(streams genericclioptions.IOStreams, repo *git.Repository, repoPath string, upstreamName, startingKubeVersion, originVersion string, openshiftRemoteConfig *config.RemoteConfig) error {
	startingKubeTag := kubefork.UpstreamTag(upstreamName, startingKubeVersion)
	originBranchName := kubefork.NewForkBranch("origin", originVersion, startingKubeVersion).BranchName()

	allReferences, err := repo.References()
	if err != nil {
		return err
	}

	openshiftBranches := []*plumbing.Reference{}
	err = allReferences.ForEach(func(ref *plumbing.Reference) error {
		switch {
		case ref.Strings()[0] == "refs/remotes/openshift/master" || strings.HasPrefix(ref.Strings()[0], "refs/remotes/openshift/release-"):
			openshiftBranches = append(openshiftBranches, ref)

		default:
			return nil
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, openshiftBranch := range openshiftBranches {
		openshiftBranchName := openshiftBranch.Strings()[0][len("refs/remotes/openshift/"):]
		if openshiftBranchName == originBranchName {
			fmt.Fprintf(streams.Out, "For kubernetes/%v, branch %q already exists, doing nothing\n", upstreamName, originBranchName)
			return nil
		}
	}

	fmt.Fprintf(streams.Out, "For kubernetes/%v, pushing %q to %q\n", upstreamName, originBranchName, openshiftRemoteConfig.Name)
	if err := kubefork.RunCmd(streams, repoPath, "git", "checkout", "-b", originBranchName, startingKubeTag); err != nil {
		return err
	}

	// the built-in reset and cleanoptions don't seem to work.  other weird behavior is mentioned in issues
	if err := kubefork.RunCmd(streams, repoPath, "git", "reset", "--hard", startingKubeTag); err != nil {
		return err
	}
	if err := kubefork.RunCmd(streams, repoPath, "git", "clean", "-fd"); err != nil {
		return err
	}

	// push to openshift
	if err := kubefork.RunCmd(streams, repoPath, "git", "push", "openshift"); err != nil {
		return err
	}

	return nil
}
