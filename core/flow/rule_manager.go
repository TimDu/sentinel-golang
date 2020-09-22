package flow

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/alibaba/sentinel-golang/logging"
	"github.com/alibaba/sentinel-golang/util"
	"github.com/pkg/errors"
)

// TrafficControllerGenFunc represents the TrafficShapingController generator function of a specific control behavior.
type TrafficControllerGenFunc func(*Rule) *TrafficShapingController

type trafficControllerGenKey struct {
	tokenCalculateStrategy TokenCalculateStrategy
	controlBehavior        ControlBehavior
}

// TrafficControllerMap represents the map storage for TrafficShapingController.
type TrafficControllerMap map[string][]*TrafficShapingController

var (
	tcGenFuncMap = make(map[trafficControllerGenKey]TrafficControllerGenFunc)
	tcMap        = make(TrafficControllerMap)
	tcMux        = new(sync.RWMutex)
)

func init() {
	// Initialize the traffic shaping controller generator map for existing control behaviors.
	tcGenFuncMap[trafficControllerGenKey{
		tokenCalculateStrategy: Direct,
		controlBehavior:        Reject,
	}] = func(rule *Rule) *TrafficShapingController {
		return NewTrafficShapingController(NewDirectTrafficShapingCalculator(rule.Count), NewDefaultTrafficShapingChecker(rule), rule)
	}
	tcGenFuncMap[trafficControllerGenKey{
		tokenCalculateStrategy: Direct,
		controlBehavior:        Throttling,
	}] = func(rule *Rule) *TrafficShapingController {
		return NewTrafficShapingController(NewDirectTrafficShapingCalculator(rule.Count), NewThrottlingChecker(rule.MaxQueueingTimeMs), rule)
	}
	tcGenFuncMap[trafficControllerGenKey{
		tokenCalculateStrategy: WarmUp,
		controlBehavior:        Reject,
	}] = func(rule *Rule) *TrafficShapingController {
		return NewTrafficShapingController(NewWarmUpTrafficShapingCalculator(rule), NewDefaultTrafficShapingChecker(rule), rule)
	}
	tcGenFuncMap[trafficControllerGenKey{
		tokenCalculateStrategy: WarmUp,
		controlBehavior:        Throttling,
	}] = func(rule *Rule) *TrafficShapingController {
		return NewTrafficShapingController(NewWarmUpTrafficShapingCalculator(rule), NewThrottlingChecker(rule.MaxQueueingTimeMs), rule)
	}
}

func logRuleUpdate(m TrafficControllerMap) {
	bs, err := json.Marshal(rulesFrom(m))
	if err != nil {
		if len(m) == 0 {
			logging.Info("[FlowRuleManager] Flow rules were cleared")
		} else {
			logging.Info("[FlowRuleManager] Flow rules were loaded")
		}
	} else {
		if len(m) == 0 {
			logging.Info("[FlowRuleManager] Flow rules were cleared")
		} else {
			logging.Infof("[FlowRuleManager] Flow rules were loaded: %s", bs)
		}
	}
}

func onRuleUpdate(rules []*Rule) (ret bool, err error, failedRules []*Rule) {
	var start uint64
	var m TrafficControllerMap
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("%v", r)
			}
			failedRules = rules
			return
		}
		logging.Debugf("Updating flow rule spends %d ns.", util.CurrentTimeNano() - start)
		logRuleUpdate(m)
	}()

	m, failedRules = buildFlowMap(rules)

	start = util.CurrentTimeNano()
	tcMux.Lock()
	defer tcMux.Unlock()

	// Check if there will be rule changes in traffic controller map
	if len(tcMap) != len(m) {
		ret = true
	} else {
		for res, tcs := range tcMap {
			mTcs, ok := m[res]
			if !ok {
				ret = true
				break
			}
			if len(tcs) != len(mTcs) {
			    ret = true
			    break
			}
			eqCount := 0
			for _, tc := range tcs {
				for _, mTc := range mTcs {
					if tc.rule.equalsTo(mTc.rule) {
						eqCount++
						break
					}
				}
			}
			// If not every current rule can find a match in new rule, then we must be
			// updating some different rules
			if eqCount < len(tcs) {
			    ret = true
			    break
			}
		}
	}

	tcMap = m
	return
}

// LoadRules loads the given flow rules to the rule manager, while all previous rules will be replaced.
//
// return value:
//
// bool: Indicates whether loading succeeds. Return false if rules are same with the effective ones; otherwise, true. 
// error: Errors. Loading will not happen if not nil.
// []*Rule: failed rule list. It would be same as input rules if an error happens.
func LoadRules(rules []*Rule) (bool, error, []*Rule) {
	ret, err, failedRules := onRuleUpdate(rules)
	return ret, err, failedRules
}

// getRules returns all the rules。Any changes of rules take effect for flow module
// getRules is an internal interface.
func getRules() []*Rule {
	tcMux.RLock()
	defer tcMux.RUnlock()

	return rulesFrom(tcMap)
}

// getRulesOfResource returns specific resource's rules。Any changes of rules take effect for flow module
// getRulesOfResource is an internal interface.
func getRulesOfResource(res string) []*Rule {
	tcMux.RLock()
	defer tcMux.RUnlock()

	resTcs, exist := tcMap[res]
	if !exist {
		return nil
	}
	ret := make([]*Rule, 0, len(resTcs))
	for _, tc := range resTcs {
		ret = append(ret, tc.Rule())
	}
	return ret
}

// GetRules returns all the rules based on copy.
// It doesn't take effect for flow module if user changes the rule.
func GetRules() []Rule {
	rules := getRules()
	ret := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		ret = append(ret, *rule)
	}
	return ret
}

// GetRulesOfResource returns specific resource's rules based on copy.
// It doesn't take effect for flow module if user changes the rule.
func GetRulesOfResource(res string) []Rule {
	rules := getRulesOfResource(res)
	ret := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		ret = append(ret, *rule)
	}
	return ret
}

// ClearRules clears all the rules in flow module.
func ClearRules() error {
	_, err, _ := LoadRules(nil)
	return err
}

func rulesFrom(m TrafficControllerMap) []*Rule {
	rules := make([]*Rule, 0)
	if len(m) == 0 {
		return rules
	}
	for _, rs := range m {
		if len(rs) == 0 {
			continue
		}
		for _, r := range rs {
			if r != nil && r.Rule() != nil {
				rules = append(rules, r.Rule())
			}
		}
	}
	return rules
}

// SetTrafficShapingGenerator sets the traffic controller generator for the given TokenCalculateStrategy and ControlBehavior.
// Note that modifying the generator of default control strategy is not allowed.
func SetTrafficShapingGenerator(tokenCalculateStrategy TokenCalculateStrategy, controlBehavior ControlBehavior, generator TrafficControllerGenFunc) error {
	if generator == nil {
		return errors.New("nil generator")
	}

	if tokenCalculateStrategy >= Direct && tokenCalculateStrategy <= WarmUp {
		return errors.New("not allowed to replace the generator for default control strategy")
	}
	if controlBehavior >= Reject && controlBehavior <= Throttling {
		return errors.New("not allowed to replace the generator for default control strategy")
	}
	tcMux.Lock()
	defer tcMux.Unlock()

	tcGenFuncMap[trafficControllerGenKey{
		tokenCalculateStrategy: tokenCalculateStrategy,
		controlBehavior:        controlBehavior,
	}] = generator
	return nil
}

func RemoveTrafficShapingGenerator(tokenCalculateStrategy TokenCalculateStrategy, controlBehavior ControlBehavior) error {
	if tokenCalculateStrategy >= Direct && tokenCalculateStrategy <= WarmUp {
		return errors.New("not allowed to replace the generator for default control strategy")
	}
	if controlBehavior >= Reject && controlBehavior <= Throttling {
		return errors.New("not allowed to replace the generator for default control strategy")
	}
	tcMux.Lock()
	defer tcMux.Unlock()

	delete(tcGenFuncMap, trafficControllerGenKey{
		tokenCalculateStrategy: tokenCalculateStrategy,
		controlBehavior:        controlBehavior,
	})
	return nil
}

func getTrafficControllerListFor(name string) []*TrafficShapingController {
	tcMux.RLock()
	defer tcMux.RUnlock()

	return tcMap[name]
}

// NotThreadSafe (should be guarded by the lock)
func buildFlowMap(rules []*Rule) (m TrafficControllerMap, failedRules []*Rule) {
	m = make(TrafficControllerMap)
	if len(rules) == 0 {
		return
	}

	for _, rule := range rules {
		if rule == nil {
			continue
		}
		if err := IsValidRule(rule); err != nil {
			failedRules = append(failedRules, rule)
			logging.Warnf("Ignoring invalid flow rule: %v, reason: %s", rule, err.Error())
			continue
		}
		rulesOfRes, exists := m[rule.Resource]

		if exists {
			// Deduplicate input rules
			for _, tc := range rulesOfRes {
				if rule.equalsTo(tc.rule) {
				    rule = nil
				    break
				}
			}
			if rule == nil {
			    continue
			}
		}

		generator, supported := tcGenFuncMap[trafficControllerGenKey{
			tokenCalculateStrategy: rule.TokenCalculateStrategy,
			controlBehavior:        rule.ControlBehavior,
		}]
		if !supported {
			failedRules = append(failedRules, rule)
			logging.Warnf("Ignoring the rule due to unsupported control behavior: %v", rule)
			continue
		}
		tsc := generator(rule)
		if tsc == nil {
			failedRules = append(failedRules, rule)
			logging.Warnf("Ignoring the rule due to bad generated traffic controller: %v", rule)
			continue
		}

		if !exists {
			m[rule.Resource] = []*TrafficShapingController{tsc}
		} else {
			m[rule.Resource] = append(rulesOfRes, tsc)
		}
	}
	return
}

// IsValidRule checks whether the given Rule is valid.
func IsValidRule(rule *Rule) error {
	if rule == nil {
		return errors.New("nil Rule")
	}
	if rule.Resource == "" {
		return errors.New("empty resource name")
	}
	if rule.Count < 0 {
		return errors.New("negative threshold")
	}
	if rule.MetricType < 0 {
		return errors.New("invalid metric type")
	}
	if rule.RelationStrategy < 0 {
		return errors.New("invalid relation strategy")
	}
	if rule.TokenCalculateStrategy < 0 || rule.ControlBehavior < 0 {
		return errors.New("invalid control strategy")
	}

	if rule.RelationStrategy == AssociatedResource && rule.RefResource == "" {
		return errors.New("Bad flow rule: invalid control behavior")
	}

	if rule.TokenCalculateStrategy == WarmUp {
		if rule.WarmUpPeriodSec <= 0 {
			return errors.New("invalid warmUpPeriodSec")
		}
		if rule.WarmUpColdFactor == 1 {
			return errors.New("WarmUpColdFactor must be great than 1")
		}
	}
	return nil
}
