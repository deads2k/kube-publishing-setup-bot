package kubefork

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/openshift/kube-publishing-setup-bot/pkg/genericclioptions"
	"github.com/src-d/go-billy/osfs"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
	"gopkg.in/src-d/go-git.v4/storage"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
)

func GetAllKubeRepos(streams genericclioptions.IOStreams, kubeHome string) ([]RepoInfo, error) {
	if err := os.MkdirAll(kubeHome, 0755); err != nil {
		return nil, err
	}
	currPath := path.Join(kubeHome, "kubernetes")
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

		if err := CloneRepo(streams.Indent(), currInfo); err != nil {
			return nil, err
		}
		if _, _, err := FetchUpdates(streams.Indent(), currInfo); err != nil {
			return nil, err
		}

	}
	// look up everything in the staging folder to prime the next repoInfos
	if repoInfos[0].UpstreamName == "kubernetes" {
		var err error
		stagingRepos, err := GetRepoInfoForStaging(streams, repoInfos[0])
		if err != nil {
			return nil, err
		}
		repoInfos = append(repoInfos, stagingRepos...)
	}

	return repoInfos, nil
}

type RepoInfo struct {
	Path          string
	RepoStorage   storage.Storer
	UpstreamName  string
	Upstream      *config.RemoteConfig
	OpenshiftName string
	Openshift     *config.RemoteConfig
}

func GetRepoInfoForStaging(streams genericclioptions.IOStreams, currInfo RepoInfo) ([]RepoInfo, error) {
	fmt.Fprintf(streams.Out, "For kubernetes/%v, checking staging for more repos\n", currInfo.UpstreamName)
	cmdStreams := streams.Indent()

	if err := RunCmd(cmdStreams, currInfo.Path, "git", "checkout", "master"); err != nil {
		return nil, err
	}
	// the built-in reset and cleanoptions don't seem to work.  other weird behavior is mentioned in issues
	if err := RunCmd(cmdStreams, currInfo.Path, "git", "reset", "--hard", "upstream/master"); err != nil {
		return nil, err
	}
	if err := RunCmd(cmdStreams, currInfo.Path, "git", "clean", "-fd"); err != nil {
		return nil, err
	}

	ret := []RepoInfo{}
	names := []string{}
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
		names = append(names, repoName)
		if repoName == "cri-api" || repoName == "kubectl" || repoName == "legacy-cloud-providers" { // need repo
			return nil
		}
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

	fmt.Fprintf(cmdStreams.Out, "Found staging repos: %v\n", strings.Join(names, " "))
	return ret, nil
}

func FetchUpdates(streams genericclioptions.IOStreams, currInfo RepoInfo) (*git.Remote, *git.Remote, error) {
	fmt.Fprintf(streams.Out, "For kubernetes/%v, fetching current code\n", currInfo.UpstreamName)

	// fetch the current state of all branches upstream and in openshift
	upstreamRemote, err := currInfo.GetOrCreateRemoteKube(streams.Indent())
	if err != nil {
		return nil, nil, err
	}
	if err := fetch(streams.Out, upstreamRemote, currInfo.UpstreamName, currInfo.Upstream); err != nil {
		return nil, nil, err
	}
	openshiftRemote, err := currInfo.GetOrCreateRemoteOpenShift(streams.Indent())
	if err != nil {
		return nil, nil, err
	}
	if err := fetch(streams.Out, openshiftRemote, currInfo.UpstreamName, currInfo.Openshift); err != nil {
		return nil, nil, err
	}

	return upstreamRemote, openshiftRemote, nil
}

func RunCmd(streams genericclioptions.IOStreams, cwd string, cmdName string, args ...string) error {
	fmt.Fprintf(streams.Out, "pushd %q && %s %s; popd\n", cwd, cmdName, strings.Join(args, " "))
	cmdStreams := streams.Indent()

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = cwd
	cmd.Stdout = cmdStreams.Out
	cmd.Stderr = cmdStreams.ErrOut
	return cmd.Run()
}

func (o *RepoInfo) GetOrCreateRemoteKube(streams genericclioptions.IOStreams) (*git.Remote, error) {
	repo, err := git.PlainOpen(o.Path)
	if err != nil {
		return nil, err
	}

	// fetch the current state of all branches upstream and in openshift
	upstreamRemote, err := getOrCreateRemote(streams.Out, repo, o.UpstreamName, o.Upstream)
	if err != nil {
		return nil, err
	}
	// ensure that we never push to kube
	if err := RunCmd(streams, o.Path, "git", "remote", "set-url", "--push", "upstream", "NO NO NO"); err != nil {
		return nil, err
	}

	return upstreamRemote, nil
}

func (o *RepoInfo) GetOrCreateRemoteOpenShift(streams genericclioptions.IOStreams) (*git.Remote, error) {
	repo, err := git.PlainOpen(o.Path)
	if err != nil {
		return nil, err
	}

	// fetch the current state of all branches upstream and in openshift
	return getOrCreateRemote(streams.Out, repo, o.UpstreamName, o.Openshift)
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

func CloneRepo(streams genericclioptions.IOStreams, currInfo RepoInfo) error {
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
	cmd.Stdout = streams.Out
	cmd.Stderr = streams.ErrOut

	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}
