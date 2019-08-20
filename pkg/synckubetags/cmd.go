package synckubetags

import (
	"fmt"
	"strings"

	"github.com/openshift/kube-publishing-setup-bot/pkg/kubefork"

	"gopkg.in/src-d/go-git.v4/plumbing"

	"github.com/openshift/kube-publishing-setup-bot/pkg/genericclioptions"
	"github.com/spf13/cobra"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
)

type SyncTagsOptions struct {
	Streams genericclioptions.IOStreams

	KubeHome string
}

func NewSyncTagsOptions(streams genericclioptions.IOStreams) *SyncTagsOptions {
	return &SyncTagsOptions{
		Streams:  streams,
		KubeHome: "kube-publishing-setup-bot.local/src/k8s.io",
	}
}

// NewCmdCreateClusterQuota is a macro command to create a new cluster quota.
func NewCmdSyncTags(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewSyncTagsOptions(streams)
	cmd := &cobra.Command{
		Use: "sync-kube-tags --kube-home=/path/to/k8s.io",
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

	return cmd
}

func (o *SyncTagsOptions) Run() error {
	repoInfos, err := kubefork.GetAllKubeRepos(o.Streams, o.KubeHome)
	if err != nil {
		return err
	}

	for i := range repoInfos {
		currInfo := repoInfos[i]

		if err := kubefork.CloneRepo(o.Streams.Indent(), currInfo); err != nil {
			return err
		}
		if err := FetchUpdates(o.Streams.Indent(), currInfo); err != nil {
			return err
		}
	}

	return nil
}

func FetchUpdates(streams genericclioptions.IOStreams, currInfo kubefork.RepoInfo) error {
	fmt.Fprintf(streams.Out, "For kubernetes/%v, reconciling tags\n", currInfo.UpstreamName)

	repo, err := git.PlainOpen(currInfo.Path)
	if err != nil {
		return err
	}

	// fetch the current state of all branches upstream and in openshift
	upstreamRemote, openshiftRemote, err := kubefork.FetchUpdates(streams.Indent(), currInfo)
	if err != nil {
		return err
	}

	// update fork branches to match upstream
	if err := pushBranches(streams.Indent(), repo, currInfo.Path, currInfo.UpstreamName, currInfo.Openshift); err != nil {
		return err
	}
	// push tags to openshift forks
	if err := pushTags(streams.Indent(), repo, currInfo.Path, upstreamRemote, openshiftRemote, currInfo.UpstreamName, currInfo.Openshift); err != nil {
		return err
	}

	return nil
}

func pushTags(streams genericclioptions.IOStreams, repo *git.Repository, repoPath string, upstreamRemote, openshiftRemote *git.Remote, upstreamName string, remoteConfig *config.RemoteConfig) error {
	// this is actually slower than pushing them all
	//upstreamTags := []*plumbing.Reference{}
	//openshiftTags := []*plumbing.Reference{}
	//wg := sync.WaitGroup{}
	//wg.Add(2)
	//
	//go func() {
	//	fmt.Fprintf(streams.Out, "For kubernetes/%v, fetch upstream tags\n", upstreamName)
	//	refs, err := upstreamRemote.List(&git.ListOptions{})
	//	if err != nil {
	//		log.Fatal(err)
	//	}
	//	for _, ref := range refs {
	//		if ref.Name().IsTag() {
	//			upstreamTags = append(upstreamTags, ref)
	//		}
	//	}
	//	fmt.Fprintf(streams.Out, "For kubernetes/%v, fetched upstream tags\n", upstreamName)
	//	wg.Done()
	//}()
	//go func() {
	//	fmt.Fprintf(streams.Out, "For kubernetes/%v, fetch openshift tags\n", upstreamName)
	//	refs, err := openshiftRemote.List(&git.ListOptions{})
	//	if err != nil {
	//		log.Fatal(err)
	//	}
	//	for _, ref := range refs {
	//		if ref.Name().IsTag() {
	//			openshiftTags = append(openshiftTags, ref)
	//		}
	//	}
	//	fmt.Fprintf(streams.Out, "For kubernetes/%v, fetched openshift tags\n", upstreamName)
	//	wg.Done()
	//}()
	//wg.Wait()
	//
	//for _, upstreamTag := range upstreamTags {
	//	matched := false
	//	for _, openshiftTag := range openshiftTags {
	//		if upstreamTag == openshiftTag {
	//			matched = true
	//		}
	//	}
	//	if matched {
	//		fmt.Fprintf(streams.Out, "For kubernetes/%v, tag %q is already up to date\n", upstreamName, upstreamTag.Name().Short())
	//		continue
	//	}
	//
	//	fmt.Fprintf(streams.Out, "For kubernetes/%v, tagging %q\n", upstreamName, upstreamTag.Name().Short())
	//	if err := kubefork.RunCmd(streams, repoPath, "git", "push", "openshift", upstreamTag.Name().Short()); err != nil {
	//		return err
	//	}
	//}

	fmt.Fprintf(streams.Out, "For kubernetes/%v, pushing all tags\n", upstreamName)
	if err := kubefork.RunCmd(streams, repoPath, "git", "push", "openshift", "--tags"); err != nil {
		return err
	}

	return nil
}

func pushBranches(streams genericclioptions.IOStreams, repo *git.Repository, repoPath string, upstreamName string, remoteConfig *config.RemoteConfig) error {
	allReferences, err := repo.References()
	if err != nil {
		return err
	}

	upstreamBranches := []*plumbing.Reference{}
	openshiftBranches := []*plumbing.Reference{}
	err = allReferences.ForEach(func(ref *plumbing.Reference) error {
		switch {
		case ref.Strings()[0] == "refs/remotes/upstream/master" || strings.HasPrefix(ref.Strings()[0], "refs/remotes/upstream/release-"):
			upstreamBranches = append(upstreamBranches, ref)

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

	for _, upstreamBranch := range upstreamBranches {
		branchName := upstreamBranch.Strings()[0][len("refs/remotes/upstream/"):]
		sha := upstreamBranch.Strings()[1]
		matched := false
		for _, openshiftBranch := range openshiftBranches {
			openshiftBranchName := openshiftBranch.Strings()[0][len("refs/remotes/openshift/"):]
			openshiftSHA := openshiftBranch.Strings()[1]
			if openshiftBranchName == branchName && openshiftSHA == sha {
				matched = true
			}
		}
		if matched {
			fmt.Fprintf(streams.Out, "For kubernetes/%v, branch %q is already up to date\n", upstreamName, branchName)
			continue
		}

		fmt.Fprintf(streams.Out, "For kubernetes/%v, pushing %q to %q\n", upstreamName, branchName, remoteConfig.Name)
		if err := kubefork.RunCmd(streams, repoPath, "git", "checkout", branchName); err != nil {
			if err := kubefork.RunCmd(streams, repoPath, "git", "checkout", "upstream/"+branchName); err != nil {
				return err
			}
			if err := kubefork.RunCmd(streams, repoPath, "git", "checkout", "-b", branchName); err != nil {
				return err
			}
		}

		// the built-in reset and cleanoptions don't seem to work.  other weird behavior is mentioned in issues
		if err := kubefork.RunCmd(streams, repoPath, "git", "reset", "--hard", sha); err != nil {
			return err
		}
		if err := kubefork.RunCmd(streams, repoPath, "git", "clean", "-fd"); err != nil {
			return err
		}

		// push to openshift
		if err := kubefork.RunCmd(streams, repoPath, "git", "push", "openshift"); err != nil {
			return err
		}
	}

	return nil
}
