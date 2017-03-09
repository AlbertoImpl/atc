// This file was generated by counterfeiter
package workerfakes

import (
	"sync"

	gconn "code.cloudfoundry.org/garden/client/connection"
	"github.com/concourse/atc/worker"
)

type FakeGardenConnectionFactory struct {
	BuildConnectionStub        func() gconn.Connection
	buildConnectionMutex       sync.RWMutex
	buildConnectionArgsForCall []struct{}
	buildConnectionReturns     struct {
		result1 gconn.Connection
	}
	buildConnectionReturnsOnCall map[int]struct {
		result1 gconn.Connection
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeGardenConnectionFactory) BuildConnection() gconn.Connection {
	fake.buildConnectionMutex.Lock()
	ret, specificReturn := fake.buildConnectionReturnsOnCall[len(fake.buildConnectionArgsForCall)]
	fake.buildConnectionArgsForCall = append(fake.buildConnectionArgsForCall, struct{}{})
	fake.recordInvocation("BuildConnection", []interface{}{})
	fake.buildConnectionMutex.Unlock()
	if fake.BuildConnectionStub != nil {
		return fake.BuildConnectionStub()
	}
	if specificReturn {
		return ret.result1
	}
	return fake.buildConnectionReturns.result1
}

func (fake *FakeGardenConnectionFactory) BuildConnectionCallCount() int {
	fake.buildConnectionMutex.RLock()
	defer fake.buildConnectionMutex.RUnlock()
	return len(fake.buildConnectionArgsForCall)
}

func (fake *FakeGardenConnectionFactory) BuildConnectionReturns(result1 gconn.Connection) {
	fake.BuildConnectionStub = nil
	fake.buildConnectionReturns = struct {
		result1 gconn.Connection
	}{result1}
}

func (fake *FakeGardenConnectionFactory) BuildConnectionReturnsOnCall(i int, result1 gconn.Connection) {
	fake.BuildConnectionStub = nil
	if fake.buildConnectionReturnsOnCall == nil {
		fake.buildConnectionReturnsOnCall = make(map[int]struct {
			result1 gconn.Connection
		})
	}
	fake.buildConnectionReturnsOnCall[i] = struct {
		result1 gconn.Connection
	}{result1}
}

func (fake *FakeGardenConnectionFactory) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.buildConnectionMutex.RLock()
	defer fake.buildConnectionMutex.RUnlock()
	return fake.invocations
}

func (fake *FakeGardenConnectionFactory) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ worker.GardenConnectionFactory = new(FakeGardenConnectionFactory)
