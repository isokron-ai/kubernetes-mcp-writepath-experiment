package core

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type PodsLogSuite struct {
	suite.Suite
}

func (s *PodsLogSuite) TestKubeletLogFailure() {
	s.True(kubeletLogFailure("unable to retrieve container logs for containerd://92a45e39097b"))
	s.True(kubeletLogFailure("unable to retrieve container logs for docker://abc\n"))
	s.False(kubeletLogFailure("worker starting\nunable to retrieve container logs for x"))
	s.False(kubeletLogFailure("connecting to db"))
}

func TestPodsLog(t *testing.T) {
	suite.Run(t, new(PodsLogSuite))
}
