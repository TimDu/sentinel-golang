package flow

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetAndRemoveTrafficShapingGenerator(t *testing.T) {
	assert := assert.New(t)
	tsc := &TrafficShapingController{}

	err := SetTrafficShapingGenerator(Direct, Reject, func(_ *Rule) *TrafficShapingController {
		return tsc
	})
	assert.Error(err, "default control behaviors are not allowed to be modified")
	err = RemoveTrafficShapingGenerator(Direct, Reject)
	assert.Error(err, "default control behaviors are not allowed to be removed")

	err = SetTrafficShapingGenerator(TokenCalculateStrategy(111), ControlBehavior(112), func(_ *Rule) *TrafficShapingController {
		return tsc
	})
	assert.NoError(err)

	resource := "test-customized-tc"
	_, err, _ = LoadRules([]*Rule{
		{
			Count:			20,
			MetricType:		QPS,
			Resource:		resource,
			TokenCalculateStrategy: TokenCalculateStrategy(111),
			ControlBehavior:	ControlBehavior(112),
		},
	})

	cs := trafficControllerGenKey{
		tokenCalculateStrategy: TokenCalculateStrategy(111),
		controlBehavior:	ControlBehavior(112),
	}
	assert.NoError(err)
	assert.Contains(tcGenFuncMap, cs)
	assert.NotZero(len(tcMap[resource]))
	assert.Equal(tsc, tcMap[resource][0])

	err = RemoveTrafficShapingGenerator(TokenCalculateStrategy(111), ControlBehavior(112))
	assert.NoError(err)
	assert.NotContains(tcGenFuncMap, cs)

	_, _, _ = LoadRules([]*Rule{})
}

func TestIsValidFlowRule(t *testing.T) {
	assert := assert.New(t)
	badRule1 := &Rule{Count: 1, MetricType: QPS, Resource: ""}
	badRule2 := &Rule{Count: -1.9, MetricType: QPS, Resource: "test"}
	badRule3 := &Rule{Count: 5, MetricType: QPS, Resource: "test", TokenCalculateStrategy: WarmUp, ControlBehavior: Reject}
	goodRule1 := &Rule{Count: 10, MetricType: QPS, Resource: "test", TokenCalculateStrategy: WarmUp, ControlBehavior: Throttling, WarmUpPeriodSec: 10, WarmUpColdFactor: 2}

	assert.Error(IsValidRule(badRule1))
	assert.Error(IsValidRule(badRule2))
	assert.Error(IsValidRule(badRule3))
	assert.NoError(IsValidRule(goodRule1))
}

func TestRuleEqualsTo(t *testing.T) {
	assert := assert.New(t)

	t.Run("equalsTo_resourceDifferent", func(t *testing.T) {
		r1 := &Rule{
			Resource:		"abc1",
			MetricType:		0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		r2 := &Rule{
			Resource:		"abc2",
			MetricType:		0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}

		assert.False(r1.equalsTo(r2))
	})
	t.Run("equalsTo_strategyDifferent", func(t *testing.T) {
		r1 := &Rule{
			Count: 10,
			MetricType: QPS,
			Resource: "test",
			TokenCalculateStrategy: WarmUp,
			ControlBehavior: Throttling,
			WarmUpPeriodSec: 10,
			WarmUpColdFactor: 2,
		}
		r2 := &Rule{
			Count: 10,
			MetricType: QPS,
			Resource: "test",
			TokenCalculateStrategy: WarmUp,
			ControlBehavior: Throttling,
			WarmUpPeriodSec: 8,
			WarmUpColdFactor: 2,
		}

		assert.False(r1.equalsTo(r2))
	})
	t.Run("equalsTo_good", func(t *testing.T) {
		r := &Rule{
			Count: 10,
			MetricType: Concurrency,
			Resource: "test",
			TokenCalculateStrategy: WarmUp,
			ControlBehavior: Throttling,
			WarmUpPeriodSec: 10,
			WarmUpColdFactor: 2,
		}

		assert.True(r.equalsTo(r))
	})
}

func Test_onRuleUpdate_valid(t *testing.T) {
	assert := assert.New(t)

	t.Run("onRuleUpdate_basic", func(t *testing.T) {
		r1 := &Rule{
			Resource:		"abc1",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		r2 := &Rule{
			Resource:		"abc2",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Throttling,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		ret, err, failedRules := onRuleUpdate([]*Rule{r1, r2})
		assert.True(ret)
		assert.NoError(err)
		assert.Empty(failedRules)
		assert.Len(tcMap["abc1"], 1)
		assert.Len(tcMap["abc2"] , 1)

		tcMap = make(TrafficControllerMap)
	})
	t.Run("onRuleUpdate_duplicate", func(t *testing.T) {
		r := &Rule{
			Resource:		"abc",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		ret, err, failedRules := onRuleUpdate([]*Rule{r, r})
		assert.True(ret)
		assert.NoError(err)
		assert.Empty(failedRules)
		assert.Len(tcMap["abc"], 1)

		tcMap = make(TrafficControllerMap)
	})
	t.Run("onRuleUpdate_repeat", func(t *testing.T) {
		r := &Rule{
			Resource:		"abc",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		_, err, _ := onRuleUpdate([]*Rule{r})
		assert.NoError(err)
		ret, err, failedRules := onRuleUpdate([]*Rule{r})
		assert.False(ret)
		assert.NoError(err)
		assert.Empty(failedRules)
		assert.Len(tcMap["abc"], 1)

		tcMap = make(TrafficControllerMap)
	})
	t.Run("onRuleUpdate_remove", func(t *testing.T) {
		r1 := &Rule{
			Resource:		"abc1",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		r2 := &Rule{
			Resource:		"abc2",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Throttling,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		_, err, _ := onRuleUpdate([]*Rule{r1, r2})
		assert.NoError(err)
		ret, err, failedRules := onRuleUpdate([]*Rule{r2})
		assert.True(ret)
		assert.NoError(err)
		assert.Empty(failedRules)
		assert.Len(tcMap, 1)
		assert.Len(tcMap["abc2"], 1)

		tcMap = make(TrafficControllerMap)
	})
	t.Run("onRuleUpdate_clear", func(t *testing.T) {
		r := &Rule{
			Resource:		"abc",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		_, err, _ := onRuleUpdate([]*Rule{r})
		assert.NoError(err)
		ret, err, failedRules := onRuleUpdate(nil)
		assert.True(ret)
		assert.NoError(err)
		assert.Empty(failedRules)
		assert.Empty(tcMap)
	})
}

func Test_onRuleUpdate_invalid(t *testing.T) {
	assert := assert.New(t)

	t.Run("onRuleUpdate_invalidRule", func(t *testing.T) {
		r := &Rule{Count: 1, MetricType: QPS, Resource: ""}
		ret, err, failedRules := onRuleUpdate([]*Rule{r})
		assert.Empty(tcMap)
		assert.False(ret)
		assert.NoError(err)
		assert.Len(failedRules, 1)
	})
	t.Run("buildFlowMap_unsupportedControlBehavior", func(t *testing.T) {
		r := &Rule{
			Count:			20,
			MetricType:		QPS,
			Resource:		"test",
			TokenCalculateStrategy: TokenCalculateStrategy(111),
			ControlBehavior:	ControlBehavior(112),
		}
		ret, err, failedRules := onRuleUpdate([]*Rule{r})
		assert.Empty(tcMap)
		assert.False(ret)
		assert.NoError(err)
		assert.Len(failedRules, 1)
	})
}

func TestGetRules(t *testing.T) {
	assert := assert.New(t)

	t.Run("GetRules", func(t *testing.T) {
		if err := ClearRules(); err != nil {
			t.Fatal(err)
		}
		r1 := &Rule{
			Resource:		"abc1",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		r2 := &Rule{
			Resource:		"abc2",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Throttling,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		if _, err, _ := LoadRules([]*Rule{r1, r2}); err != nil {
			t.Fatal(err)
		}

		rs1 := GetRules()
		if rs1[0].Resource == "abc1" {
			assert.True(&rs1[0] != r1)
			assert.True(&rs1[1] != r2)
			assert.True(reflect.DeepEqual(&rs1[0], r1))
			assert.True(reflect.DeepEqual(&rs1[1], r2))
		} else {
			assert.True(&rs1[0] != r2)
			assert.True(&rs1[1] != r1)
			assert.True(reflect.DeepEqual(&rs1[0], r2))
			assert.True(reflect.DeepEqual(&rs1[1], r1))
		}
		if err := ClearRules(); err != nil {
			t.Fatal(err)
		}
		assert.Empty(tcMap)
	})

	t.Run("getRules", func(t *testing.T) {
		r1 := &Rule{
			Resource:		"abc1",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Reject,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		r2 := &Rule{
			Resource:		"abc2",
			MetricType:		0,
			Count:			0,
			RelationStrategy:	0,
			TokenCalculateStrategy: Direct,
			ControlBehavior:	Throttling,
			RefResource:		"",
			WarmUpPeriodSec:	0,
			MaxQueueingTimeMs:	0,
		}
		if _, err, _ := LoadRules([]*Rule{r1, r2}); err != nil {
			t.Fatal(err)
		}
		rs2 := getRules()
		if rs2[0].Resource == "abc1" {
			assert.True(rs2[0] == r1)
			assert.True(rs2[1] == r2)
			assert.True(reflect.DeepEqual(rs2[0], r1))
			assert.True(reflect.DeepEqual(rs2[1], r2))
		} else {
			assert.True(rs2[0] == r2)
			assert.True(rs2[1] == r1)
			assert.True(reflect.DeepEqual(rs2[0], r2))
			assert.True(reflect.DeepEqual(rs2[1], r1))
		}
		if err := ClearRules(); err != nil {
			t.Fatal(err)
		}
		assert.Empty(tcMap)
	})
}
