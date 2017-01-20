// This file was generated by counterfeiter
package dbngfakes

import (
	"sync"

	"github.com/concourse/atc/dbng"
)

type FakePipelineFactory struct {
	GetPipelineByIDStub        func(teamID int, pipelineID int) dbng.Pipeline
	getPipelineByIDMutex       sync.RWMutex
	getPipelineByIDArgsForCall []struct {
		teamID     int
		pipelineID int
	}
	getPipelineByIDReturns struct {
		result1 dbng.Pipeline
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakePipelineFactory) GetPipelineByID(teamID int, pipelineID int) dbng.Pipeline {
	fake.getPipelineByIDMutex.Lock()
	fake.getPipelineByIDArgsForCall = append(fake.getPipelineByIDArgsForCall, struct {
		teamID     int
		pipelineID int
	}{teamID, pipelineID})
	fake.recordInvocation("GetPipelineByID", []interface{}{teamID, pipelineID})
	fake.getPipelineByIDMutex.Unlock()
	if fake.GetPipelineByIDStub != nil {
		return fake.GetPipelineByIDStub(teamID, pipelineID)
	} else {
		return fake.getPipelineByIDReturns.result1
	}
}

func (fake *FakePipelineFactory) GetPipelineByIDCallCount() int {
	fake.getPipelineByIDMutex.RLock()
	defer fake.getPipelineByIDMutex.RUnlock()
	return len(fake.getPipelineByIDArgsForCall)
}

func (fake *FakePipelineFactory) GetPipelineByIDArgsForCall(i int) (int, int) {
	fake.getPipelineByIDMutex.RLock()
	defer fake.getPipelineByIDMutex.RUnlock()
	return fake.getPipelineByIDArgsForCall[i].teamID, fake.getPipelineByIDArgsForCall[i].pipelineID
}

func (fake *FakePipelineFactory) GetPipelineByIDReturns(result1 dbng.Pipeline) {
	fake.GetPipelineByIDStub = nil
	fake.getPipelineByIDReturns = struct {
		result1 dbng.Pipeline
	}{result1}
}

func (fake *FakePipelineFactory) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.getPipelineByIDMutex.RLock()
	defer fake.getPipelineByIDMutex.RUnlock()
	return fake.invocations
}

func (fake *FakePipelineFactory) recordInvocation(key string, args []interface{}) {
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

var _ dbng.PipelineFactory = new(FakePipelineFactory)
