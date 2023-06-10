package simulation

import (
	"fmt"

	"github.com/simimpact/srsim/pkg/engine/event"
	"github.com/simimpact/srsim/pkg/engine/info"
	"github.com/simimpact/srsim/pkg/engine/queue"
	"github.com/simimpact/srsim/pkg/engine/target"
	"github.com/simimpact/srsim/pkg/key"
	"github.com/simimpact/srsim/pkg/model"
)

// TODO: Unknown TC
//	- Does ActionStart & ActionEnd happen for inserted actions? If  so, this means
//		LC such as "In the Name of the World" buff these insert actions
//	- Do Insert abilities (follow up attacks, counters, etc) count as an Action (similar to above)?

func (sim *simulation) InsertAction(target key.TargetID) {
	var priority info.InsertPriority
	switch sim.targets[target] {
	case TargetEnemy:
		priority = info.EnemyInsertAction
	default:
		priority = info.CharInsertAction
	}

	sim.queue.Insert(queue.Task{
		Source:   target,
		Priority: priority,
		AbortFlags: []model.BehaviorFlag{
			model.BehaviorFlag_STAT_CTRL,
			model.BehaviorFlag_DISABLE_ACTION,
		},
		Execute: func() { sim.executeAction(target, true) },
	})
}

func (sim *simulation) InsertAbility(i info.Insert) {
	sim.queue.Insert(queue.Task{
		Source:     i.Source,
		Priority:   i.Priority,
		AbortFlags: i.AbortFlags,
		Execute:    func() { sim.executeInsert(i) },
	})
}

func (sim *simulation) ultCheck() {
	// TODO: enemy ult support?
	bursts := sim.eval.BurstCheck()
	for _, value := range bursts {
		// TODO: need a "burst type" for cases like MC (passed to executeUlt)
		// TODO: need a target evaluator key to be passed to executeUlt

		sim.queue.Insert(queue.Task{
			Source:   value.Target,
			Priority: info.CharInsertUlt,
			AbortFlags: []model.BehaviorFlag{
				model.BehaviorFlag_STAT_CTRL,
				model.BehaviorFlag_DISABLE_ACTION,
			},
			Execute: func() { sim.executeUlt(value.Target) },
		})
	}
}

// execute everything on the queue. After queue execution is complete, return the next stateFn
// as the next state to run. This logic will run the exitCheck on each execution. If an exit
// condition is met, will return that state instead
func (s *simulation) executeQueue(phase info.BattlePhase, next stateFn) (stateFn, error) {
	// always ult check when calling executeQueue
	s.ultCheck()

	// if active is not a character, cannot prform any queue execution until after ActionEnd
	if phase < info.ActionEnd && !s.IsCharacter(s.active) {
		return s.exitCheck(next)
	}

	for !s.queue.IsEmpty() {
		insert := s.queue.Pop()

		// if source has no HP, skip this insert
		if s.attributeService.HPRatio(insert.Source) <= 0 {
			continue
		}

		// if the source has an abort flag, skip this insert
		if s.HasBehaviorFlag(insert.Source, insert.AbortFlags...) {
			continue
		}

		insert.Execute()

		// attempt to exit. If can exit, stop sim now
		if next, err := s.exitCheck(next); next == nil || err != nil {
			return next, err
		}
		s.ultCheck()
	}

	s.skillEffect = model.SkillEffect_INVALID_SKILL_EFFECT
	return next, nil
}

func (sim *simulation) executeAction(id key.TargetID, isInsert bool) error {
	var executable target.ExecutableAction
	var err error

	switch sim.targets[id] {
	case TargetCharacter:
		executable, err = sim.charManager.ExecuteAction(id, isInsert)
		if err != nil {
			return fmt.Errorf("error building char executable action %w", err)
		}
	case TargetEnemy:
		// TODO:
	case TargetNeutral:
		// TODO:
	default:
		return fmt.Errorf("unsupported target type: %v", sim.targets[id])
	}

	sim.ModifySP(executable.SPChange)

	sim.skillEffect = executable.SkillEffect
	sim.event.ActionStart.Emit(event.ActionEvent{
		Target:      id,
		SkillEffect: executable.SkillEffect,
		AttackType:  executable.AttackType,
		IsInsert:    isInsert,
	})

	// execute action
	executable.Execute()

	// end attack if in one. no-op if not in an attack
	// emit end events
	sim.combatManager.EndAttack()
	sim.event.ActionEnd.Emit(event.ActionEvent{
		Target:      id,
		SkillEffect: executable.SkillEffect,
		AttackType:  executable.AttackType,
		IsInsert:    isInsert,
	})
	return nil
}

func (sim *simulation) executeUlt(id key.TargetID) error {
	var executable target.ExecutableUlt
	var err error

	switch sim.targets[id] {
	case TargetCharacter:
		executable, err = sim.charManager.ExecuteUlt(id)
		if err != nil {
			return fmt.Errorf("error building char executable ult %w", err)
		}
	default:
		return fmt.Errorf("unsupported target type: %v", sim.targets[id])
	}

	sim.skillEffect = executable.SkillEffect
	sim.event.UltStart.Emit(event.ActionEvent{
		Target:      id,
		SkillEffect: executable.SkillEffect,
		AttackType:  model.AttackType_ULT,
		IsInsert:    true,
	})

	executable.Execute()

	// end attack if in one. no-op if not in an attack
	sim.combatManager.EndAttack()
	sim.event.UltEnd.Emit(event.ActionEvent{
		Target:      id,
		SkillEffect: executable.SkillEffect,
		AttackType:  model.AttackType_ULT,
		IsInsert:    true,
	})
	return nil
}

func (sim *simulation) executeInsert(i info.Insert) {
	sim.skillEffect = i.SkillEffect
	sim.event.InsertStart.Emit(event.InsertEvent{
		Target:      i.Source,
		SkillEffect: i.SkillEffect,
		AbortFlags:  i.AbortFlags,
		Priority:    i.Priority,
	})

	// execute insert
	i.Execute()

	// end attack if in one. no-op if not in an attack
	sim.combatManager.EndAttack()
	sim.event.InsertEnd.Emit(event.InsertEvent{
		Target:      i.Source,
		SkillEffect: i.SkillEffect,
		AbortFlags:  i.AbortFlags,
		Priority:    i.Priority,
	})
}
