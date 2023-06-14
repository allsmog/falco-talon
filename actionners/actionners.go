package actionners

import (
	"github.com/Issif/falco-talon/actionners/checks"
	"github.com/Issif/falco-talon/actionners/kubernetes/exec"
	labelize "github.com/Issif/falco-talon/actionners/kubernetes/labelize"
	networkpolicy "github.com/Issif/falco-talon/actionners/kubernetes/networkpolicy"
	terminate "github.com/Issif/falco-talon/actionners/kubernetes/terminate"
	"github.com/Issif/falco-talon/configuration"
	"github.com/Issif/falco-talon/internal/events"
	kubernetes "github.com/Issif/falco-talon/internal/kubernetes/client"
	"github.com/Issif/falco-talon/internal/rules"
	"github.com/Issif/falco-talon/notifiers"
	"github.com/Issif/falco-talon/utils"
)

type Actionner struct {
	Name            string
	Category        string
	Action          func(rule *rules.Rule, event *events.Event) (utils.LogLine, error)
	CheckParameters func(rule *rules.Rule) error
	Init            func() error
	Checks          []checkActionner
	Continue        bool
	Before          bool
}

type checkActionner func(event *events.Event) error

type category struct {
	initialized bool
	withsuccess bool
}

type Actionners []*Actionner

var actionners *Actionners

func Init() {
	config := configuration.GetConfiguration()
	actionners = new(Actionners)
	a := new(Actionners)
	a.Add(
		&Actionner{
			Name:            "terminate",
			Category:        "kubernetes",
			Continue:        false,
			Before:          false,
			Init:            kubernetes.Init,
			Checks:          []checkActionner{kubernetes.CheckPodExist},
			CheckParameters: terminate.CheckParameters,
			Action:          terminate.Terminate,
		},
		&Actionner{
			Name:            "labelize",
			Category:        "kubernetes",
			Continue:        true,
			Before:          false,
			Init:            kubernetes.Init,
			Checks:          []checkActionner{kubernetes.CheckPodExist},
			CheckParameters: labelize.CheckParameters,
			Action:          labelize.Labelize,
		},
		&Actionner{
			Name:     "networkpolicy",
			Category: "kubernetes",
			Continue: true,
			Before:   true,
			Init:     kubernetes.Init,
			Checks: []checkActionner{
				kubernetes.CheckPodExist,
				checks.CheckRemoteIP,
				checks.CheckRemotePort,
			},
			CheckParameters: nil,
			Action:          networkpolicy.NetworkPolicy,
		},
		&Actionner{
			Name:     "exec",
			Category: "kubernetes",
			Continue: true,
			Before:   true,
			Init:     kubernetes.Init,
			Checks: []checkActionner{
				kubernetes.CheckPodExist,
			},
			CheckParameters: nil,
			Action:          exec.Exec,
		})
	categories := map[string]*category{}
	for _, i := range *a {
		categories[i.Category] = new(category)
	}
	rules := rules.GetRules()
	for _, i := range *a {
		for _, j := range *rules {
			if i.CheckParameters != nil {
				if err := i.CheckParameters(j); err != nil {
					utils.PrintLog("fatal", config.LogFormat, utils.LogLine{Error: err, Rule: j.GetName(), Message: "rules"})
				}
			}
			if i.Category == j.GetActionCategory() {
				if !categories[i.Category].initialized {
					categories[i.Category].initialized = true
					if i.Init != nil {
						utils.PrintLog("info", config.LogFormat, utils.LogLine{Message: "init", ActionCategory: i.Category})
						if err := i.Init(); err != nil {
							utils.PrintLog("error", config.LogFormat, utils.LogLine{Error: err, ActionCategory: i.Category})
							continue
						}
					}
					categories[i.Category].withsuccess = true
				}
				if categories[i.Category].withsuccess {
					actionners.Add(i)
				}
			}
		}
	}
}

func GetActionners() *Actionners {
	return actionners
}

func (actionners *Actionners) GetActionner(category, name string) *Actionner {
	for _, i := range *actionners {
		if i.Category == category && i.Name == name {
			return i
		}
	}
	return nil
}

func (actionner *Actionner) MustContinue() bool {
	return actionner.Continue
}

func (actionner *Actionner) RunBefore() bool {
	return actionner.Before
}

func (actionners *Actionners) Add(actionner ...*Actionner) {
	*actionners = append(*actionners, actionner...)
}

func Trigger(rule *rules.Rule, event *events.Event) {
	config := configuration.GetConfiguration()
	actionners := GetActionners()
	action := rule.GetAction()
	actionName := rule.GetActionName()
	category := rule.GetActionCategory()
	ruleName := rule.GetName()
	utils.PrintLog("info", config.LogFormat, utils.LogLine{Rule: ruleName, Action: action, TraceID: event.TraceID, Message: "match"})
	for _, i := range *actionners {
		if i.Category == category && i.Name == actionName {
			if len(i.Checks) != 0 {
				for _, j := range i.Checks {
					if err := j(event); err != nil {
						utils.PrintLog("error", config.LogFormat, utils.LogLine{Error: err, Rule: ruleName, Action: action, TraceID: event.TraceID, Message: "action"})
						return
					}
				}
			}
			result, err := i.Action(rule, event)
			result.Rule = ruleName
			result.Action = action
			result.TraceID = event.TraceID
			result.Message = "action"
			result.Event = event.Output
			if err != nil {
				utils.PrintLog("error", config.LogFormat, result)
			} else {
				utils.PrintLog("info", config.LogFormat, result)
			}
			notifiers.Notify(rule, event, result)
			return
		}
	}
}
