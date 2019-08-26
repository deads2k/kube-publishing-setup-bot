package kubefork

import (
	"fmt"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

func UpstreamTag(upstreamRepo, kubeVersion string) string {
	if upstreamRepo == "kubernetes" {
		return "v" + kubeVersion
	}
	return "kubernetes-" + kubeVersion
}

func NewForkBranch(owner, version, kubeVersion string) ForkBranchInfo {
	return ForkBranchInfo{
		ForkOwner:   owner,
		ForkVersion: version,
		KubeVersion: kubeVersion,
	}
}

type ForkBranchInfo struct {
	ForkOwner   string // like origin
	ForkVersion string // like 4.2
	KubeVersion string // like 1.15.0
}

func (i ForkBranchInfo) BranchName() string {
	return fmt.Sprintf("%s-%s-kubernetes-%s", i.ForkOwner, i.ForkVersion, i.KubeVersion)
}

func FindOpenShiftBranch(name string, repo *git.Repository) (*plumbing.Reference, error) {
	allReferences, err := repo.References()
	if err != nil {
		return nil, err
	}

	var ret *plumbing.Reference
	err = allReferences.ForEach(func(ref *plumbing.Reference) error {
		switch {
		case ref.Strings()[0] == "refs/remotes/openshift/"+name:
			ret = ref
		default:
			return nil
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return nil, fmt.Errorf("missing: %q", name)
	}

	return ret, nil
}

func FindKubeTag(name string, repo *git.Repository) (*plumbing.Reference, error) {
	allReferences, err := repo.References()
	if err != nil {
		return nil, err
	}

	var ret *plumbing.Reference
	err = allReferences.ForEach(func(ref *plumbing.Reference) error {
		switch {
		case ref.Strings()[0] == "refs/tags/"+name:
			ret = ref
		default:
			return nil
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return nil, fmt.Errorf("missing: %q", name)
	}

	return ret, nil
}
