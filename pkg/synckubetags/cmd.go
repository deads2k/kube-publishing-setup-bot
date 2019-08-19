package synckubetags

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/src-d/go-git.v4/plumbing"

	"github.com/openshift/kube-publishing-setup-bot/pkg/genericclioptions"
	"github.com/spf13/cobra"
	"github.com/src-d/go-billy/osfs"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
	"gopkg.in/src-d/go-git.v4/storage"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
)

type SyncTagsOptions struct {
	Streams genericclioptions.IOStreams

	KubeHome string
}

func NewCreateClusterQuotaOptions(streams genericclioptions.IOStreams) *SyncTagsOptions {
	return &SyncTagsOptions{
		Streams:  streams,
		KubeHome: "kube-publishing-setup-bot.local/src/k8s.io",
	}
}

// NewCmdCreateClusterQuota is a macro command to create a new cluster quota.
func NewCmdSyncTags(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewCreateClusterQuotaOptions(streams)
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
		Aliases: []string{"clusterquota"},
	}

	cmd.Flags().StringVar(&o.KubeHome, "kube-home", o.KubeHome, "points to /path/to/k8s.io where /path/to/k8s.io/{kubernetes,api,apimachinery,etcd} should be.")

	return cmd
}

type RepoInfo struct {
	Path          string
	RepoStorage   storage.Storer
	UpstreamName  string
	Upstream      *config.RemoteConfig
	OpenshiftName string
	Openshift     *config.RemoteConfig
}

func (o *SyncTagsOptions) Run() error {
	if err := os.MkdirAll(o.KubeHome, 0755); err != nil {
		return err
	}

	currPath := path.Join(o.KubeHome, "kubernetes")
	repoInfos := []RepoInfo{
		{
			Path:         currPath,
			RepoStorage:  filesystem.NewStorage(osfs.New(path.Join(currPath, ".git")), cache.NewObjectLRUDefault()),
			UpstreamName: "kubernetes",
			Upstream: &config.RemoteConfig{
				Name: "upstream",
				URLs: []string{fmt.Sprintf(`git@github.com:/kubernetes/%s.git`, "kubernetes")},
				Fetch: []config.RefSpec{
					"+refs/heads/*:refs/remotes/upstream/*",
				},
			},
			OpenshiftName: "kubernetes",
			Openshift: &config.RemoteConfig{
				Name: "openshift",
				URLs: []string{fmt.Sprintf(`git@github.com:/openshift/%s.git`, "kubernetes")},
				Fetch: []config.RefSpec{
					"+refs/heads/*:refs/remotes/openshift/*",
				},
			},
		},
	}

	for i := range repoInfos {
		currInfo := repoInfos[i]

		if err := o.CloneRepo(currInfo); err != nil {
			return err
		}
		if err := FetchUpdates(o.Streams, currInfo); err != nil {
			return err
		}

	}
	// look up everything in the staging folder to prime the next repoInfos
	stagingRepos := []RepoInfo{}
	if repoInfos[0].UpstreamName == "kubernetes" {
		var err error
		stagingRepos, err = getRepoInfoForStaging(o.Streams, repoInfos[0])
		if err != nil {
			return err
		}
	}

	for i := range stagingRepos {
		currInfo := stagingRepos[i]
		fmt.Println(currInfo)

		if err := o.CloneRepo(currInfo); err != nil {
			return err
		}
		if err := FetchUpdates(o.Streams, currInfo); err != nil {
			return err
		}
	}

	return nil
}

func getRepoInfoForStaging(streams genericclioptions.IOStreams, currInfo RepoInfo) ([]RepoInfo, error) {
	fmt.Fprintf(streams.Out, "For kubernetes/%v, checking staging for more repos\n", currInfo.UpstreamName)

	if err := runCmd(streams, currInfo.Path, "git", "checkout", "master"); err != nil {
		return nil, err
	}
	// the built-in reset and cleanoptions don't seem to work.  other weird behavior is mentioned in issues
	if err := runCmd(streams, currInfo.Path, "git", "reset", "--hard", "upstream/master"); err != nil {
		return nil, err
	}
	if err := runCmd(streams, currInfo.Path, "git", "clean", "-fd"); err != nil {
		return nil, err
	}

	ret := []RepoInfo{}
	err := filepath.Walk(path.Join(currInfo.Path, "staging", "src", "k8s.io"), func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		// we only care about the first level of depth from k8s.io
		parentDir := path.Dir(filePath)
		if !strings.HasSuffix(parentDir, "k8s.io/kubernetes/staging/src/k8s.io") {
			return nil
		}

		repoName := path.Base(filePath)

		// if there is a new staging repo, we may need to skip it until we get a new repo created for it.  It's probably easier to just get a new forked repo.
		// Ask James Russell in forum-dp-platform
		missingForks := map[string]bool{
			// "repo-name": true
		}
		if missingForks[repoName] { // need repo
			return nil
		}

		fmt.Println(repoName)
		currPath := path.Join(path.Dir(currInfo.Path), repoName)
		ret = append(ret,
			RepoInfo{
				Path:         currPath,
				RepoStorage:  filesystem.NewStorage(osfs.New(path.Join(currPath, ".git")), cache.NewObjectLRUDefault()),
				UpstreamName: repoName,
				Upstream: &config.RemoteConfig{
					Name: "upstream",
					URLs: []string{fmt.Sprintf(`git@github.com:/kubernetes/%s.git`, repoName)},
					Fetch: []config.RefSpec{
						"+refs/heads/*:refs/remotes/upstream/*",
					},
				},
				OpenshiftName: "kubernetes-" + repoName,
				Openshift: &config.RemoteConfig{
					Name: "openshift",
					URLs: []string{fmt.Sprintf(`git@github.com:/openshift/%s.git`, "kubernetes-"+repoName)},
					Fetch: []config.RefSpec{
						"+refs/heads/*:refs/remotes/openshift/*",
					},
				},
			},
		)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func FetchUpdates(streams genericclioptions.IOStreams, currInfo RepoInfo) error {
	fmt.Fprintf(streams.Out, "For kubernetes/%v, reconciling tags\n", currInfo.UpstreamName)

	repo, err := git.PlainOpen(currInfo.Path)
	if err != nil {
		return err
	}

	// fetch the current state of all branches upstream and in openshift
	upstreamRemote, err := getOrCreateRemote(streams.Out, repo, currInfo.UpstreamName, currInfo.Upstream)
	if err != nil {
		return err
	}
	// ensure that we never push to kube
	if err := runCmd(streams, currInfo.Path, "git", "remote", "set-url", "--push", "upstream", "NO NO NO"); err != nil {
		return err
	}
	if err := fetch(streams.Out, upstreamRemote, currInfo.UpstreamName, currInfo.Upstream); err != nil {
		return err
	}
	openshiftRemote, err := getOrCreateRemote(streams.Out, repo, currInfo.UpstreamName, currInfo.Openshift)
	if err != nil {
		return err
	}
	if err := fetch(streams.Out, openshiftRemote, currInfo.UpstreamName, currInfo.Openshift); err != nil {
		return err
	}

	// update fork branches to match upstream
	if err := pushBranches(streams, repo, currInfo.Path, openshiftRemote, currInfo.UpstreamName, currInfo.Openshift); err != nil {
		return err
	}
	// push tags to openshift forks
	if err := pushTags(streams, repo, currInfo.Path, upstreamRemote, openshiftRemote, currInfo.UpstreamName, currInfo.Openshift); err != nil {
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
	//	if err := runCmd(streams, repoPath, "git", "push", "openshift", upstreamTag.Name().Short()); err != nil {
	//		return err
	//	}
	//}

	fmt.Fprintf(streams.Out, "For kubernetes/%v, pushing all tags\n", upstreamName)
	if err := runCmd(streams, repoPath, "git", "push", "openshift", "--tags"); err != nil {
		return err
	}

	return nil
}

func pushBranches(streams genericclioptions.IOStreams, repo *git.Repository, repoPath string, remote *git.Remote, upstreamName string, remoteConfig *config.RemoteConfig) error {
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
		if err := runCmd(streams, repoPath, "git", "checkout", branchName); err != nil {
			if err := runCmd(streams, repoPath, "git", "checkout", "upstream/"+branchName); err != nil {
				return err
			}
			if err := runCmd(streams, repoPath, "git", "checkout", "-b", branchName); err != nil {
				return err
			}
		}

		// the built-in reset and cleanoptions don't seem to work.  other weird behavior is mentioned in issues
		if err := runCmd(streams, repoPath, "git", "reset", "--hard", sha); err != nil {
			return err
		}
		if err := runCmd(streams, repoPath, "git", "clean", "-fd"); err != nil {
			return err
		}

		// push to openshift
		if err := runCmd(streams, repoPath, "git", "push", "openshift"); err != nil {
			return err
		}
	}

	return nil
}

func runCmd(streams genericclioptions.IOStreams, cwd string, cmdName string, args ...string) error {
	fmt.Fprintf(streams.Out, "pushd %q && %s %s; popd\n", cwd, cmdName, strings.Join(args, " "))
	cmd := exec.Command(cmdName, args...)
	cmd.Dir = cwd
	cmd.Stdout = streams.Out
	cmd.Stderr = streams.ErrOut
	return cmd.Run()
}

func getOrCreateRemote(out io.Writer, repo *git.Repository, upstreamName string, remoteConfig *config.RemoteConfig) (*git.Remote, error) {
	remote, err := repo.Remote(remoteConfig.Name)
	if err != nil {
		fmt.Fprintf(out, "For kubernetes/%v, creating %q remote\n", upstreamName, remoteConfig.Name)
		remote, err = repo.CreateRemote(remoteConfig)
		if err != nil {
			return nil, err
		}
	}
	return remote, nil
}

func fetch(out io.Writer, remote *git.Remote, upstreamName string, remoteConfig *config.RemoteConfig) error {
	fmt.Fprintf(out, "For kubernetes/%v, getting tags for %q\n", upstreamName, remoteConfig.Name)
	err := remote.Fetch(&git.FetchOptions{
		RemoteName: remoteConfig.Name,
		RefSpecs:   remoteConfig.Fetch,
		Depth:      0,
		//Auth:       transport.AuthMethod,
		Progress: out,
		Tags:     git.AllTags,
		Force:    false,
	})
	if err != nil && git.NoErrAlreadyUpToDate != err {
		return err
	}
	return nil
}

func (o *SyncTagsOptions) CloneRepo(currInfo RepoInfo) error {
	_, err := os.Stat(currInfo.Path)
	switch {
	case err == nil:
		fmt.Printf("Found kubernetes/%v in %q, skipping clone\n", currInfo.UpstreamName, currInfo.Path)
		return nil
	case os.IsNotExist(err):
		fmt.Printf("Missing kubernetes/%v in %q, cloning \n", currInfo.UpstreamName, currInfo.Path)
	case err != nil:
		return err
	}

	cmd := exec.Command("git", "clone", fmt.Sprintf(`git@github.com:/openshift/%s.git`, currInfo.UpstreamName))
	cmd.Dir = path.Dir(currInfo.Path)
	cmd.Stdout = o.Streams.Out
	cmd.Stderr = o.Streams.ErrOut

	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}
