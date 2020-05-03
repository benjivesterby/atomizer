// Copyright © 2019 Developer Network, LLC
//
// This file is subject to the terms and conditions defined in
// file 'LICENSE', which is part of this source code package.

package atomizer

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/devnw/validator"
)

// atomizer facilitates the execution of tasks (aka Electrons) which
// are received from the configured sources these electrons can be
// distributed across many instances of the atomizer on different nodes
// in a distributed system or in memory. Atoms should be created to
// process "atomic" actions which are small in scope and overall processing
// complexity minimizing time to run and allowing for the distributed
// system to take on the burden of long running processes as a whole
// rather than a single process handling the overall load
type atomizer struct {

	// Electron Channel
	electrons chan instance

	// channel for passing the instance to a monitoring go routine
	bonded chan instance

	// This communicates the different conductors and atoms that are
	// registered into the system while it's alive
	registrations chan interface{}

	// This sync.Map contains the channels for handling each of the
	// bondings for the different atoms registered in the system
	atomsMu sync.RWMutex
	atoms   map[string]chan<- instance

	eventsMu sync.RWMutex
	events   chan interface{}

	ctx    context.Context
	cancel context.CancelFunc

	execSyncOnce sync.Once
}

// init builds the default struct instance for atomizer
// TODO: this should eventually accept a buffer argument
func (a *atomizer) init(ctx context.Context) *atomizer {

	a.ctx, a.cancel = _ctx(ctx)

	// Initialize the electrons channel
	if a.electrons == nil {
		a.electrons = make(chan instance)
	}

	// Initialize the bonded channel
	if a.bonded == nil {
		a.bonded = make(chan instance)
	}

	// Initialize the registrations channel
	if a.registrations == nil {
		a.registrations = make(chan interface{})
	}

	// Initialize the atom fan out map and mutex
	if a.atoms == nil {
		a.atoms = make(map[string]chan<- instance)
	}

	for _, r := range Registrations() {
		a.register(r)
	}

	return a
}

// If the event channel is not nil then send the event on the channel
func (a *atomizer) event(events ...interface{}) {

	a.eventsMu.RLock()
	defer a.eventsMu.RUnlock()

	if a.events != nil {
		for _, e := range events {
			if validator.Valid(e) {
				select {
				case <-a.ctx.Done():
					return
				case a.events <- e:
				}
			}
		}
	}
}

// Initialize the go routines that will read from the conductors concurrently
// while other parts of the atomizer reads in the inputs and executes the
// instances of electrons
func (a *atomizer) receive() {
	if a.registrations == nil {
		a.event(Error{
			Event: Event{
				Message: "nil registrations channel",
			},
		})
		return
	}

	// TODO: Self-heal with heartbeats
	for {
		select {
		case <-a.ctx.Done():
			return
		case r, ok := <-a.registrations:
			if !ok {
				a.event(simple("registrations closed", nil))
				return
			}

			a.register(r)
			a.event(makeEvent("registered " + ID(r)))
		}
	}
}

// register the different receivable interfaces into the atomizer from
// wherever they were sent from
func (a *atomizer) register(input interface{}) {
	if !validator.Valid(input) {
		a.event(simple("invalid registration "+ID(input), nil))
	}

	switch v := input.(type) {
	case Conductor:
		err := a.receiveConductor(v)
		if err == nil {
			a.event(Event{
				Message:     "conductor received",
				ConductorID: ID(v),
			})
		}
	case Atom:

		err := a.receiveAtom(v)
		if err == nil {
			a.event(Event{
				Message: "atom received",
				AtomID:  ID(v),
			})
		}
	default:
		a.event(simple(
			"unknown registration type "+ID(input),
			nil,
		))
	}
}

// receiveConductor setups a retrieval loop for the conductor
func (a *atomizer) receiveConductor(conductor Conductor) error {
	if !validator.Valid(conductor) {
		return Error{Event: Event{
			Message:     "invalid conductor",
			ConductorID: ID(conductor),
		}}
	}

	go a.conduct(a.ctx, conductor)

	return nil
}

// conduct reads in from a specific electron channel of a conductor and drop
// it onto the atomizer channel for electrons
func (a *atomizer) conduct(ctx context.Context, conductor Conductor) {
	// Self Heal - Re-place the conductor on the register channel for
	// the atomizer to re-initialize so this stack can be
	// garbage collected

	// 	a.event(a.Register(conductor))
	// }))

	receiver := conductor.Receive(ctx)

	// Read from the electron channel for a conductor and push onto
	// the a electron channel for processing
	for {

		select {
		case <-ctx.Done():
			return
		case e, ok := <-receiver:
			if !ok {
				a.event(Error{Event: Event{
					Message:     "receiver closed",
					ElectronID:  e.ID,
					ConductorID: ID(conductor),
				}})

				return
			}

			if !validator.Valid(e) {
				err := Error{Event: Event{
					Message:     "invalid electron",
					ElectronID:  e.ID,
					ConductorID: ID(conductor),
				}}

				err.Internal = conductor.Complete(
					ctx,
					Properties{
						ElectronID: e.ID,
						AtomID:     e.AtomID,
						Start:      time.Now(),
						End:        time.Now(),
						Error:      err,
						Result:     nil,
					},
				)

				a.event(err)
				continue
			}

			a.event(Event{
				Message:     "electron received",
				ElectronID:  e.ID,
				AtomID:      e.AtomID,
				ConductorID: ID(conductor),
			})

			// Send the electron down the electrons
			// channel to be processed
			select {
			case <-a.ctx.Done():
				return
			case a.electrons <- instance{
				electron:  e,
				conductor: conductor,
			}:
				a.event(Event{
					Message:     "electron distributed",
					ElectronID:  e.ID,
					AtomID:      e.AtomID,
					ConductorID: ID(conductor),
				})
			}
		}
	}
}

// receiveAtom setups a retrieval loop for the conductor being passed in
func (a *atomizer) receiveAtom(atom Atom) error {
	if !validator.Valid(atom) {
		return Error{
			Event: Event{
				Message: "invalid atom",
				AtomID:  ID(atom),
			},
		}
	}

	// Register the atom into the atomizer for receiving electrons
	a.atomsMu.Lock()
	defer a.atomsMu.Unlock()

	a.atoms[ID(atom)] = a.split(atom)
	a.event(Event{
		Message: "registered electron channel",
		AtomID:  ID(atom),
	})

	return nil
}

func (a *atomizer) split(atom Atom) chan<- instance {
	electrons := make(chan instance)

	go a._split(atom, electrons)

	return electrons
}

func (a *atomizer) _split(
	atom Atom,
	electrons <-chan instance,
) {
	// Read from the electron channel for a conductor and push
	// onto the a electron channel for processing
	for {
		select {
		case <-a.ctx.Done():
			return
		case inst, ok := <-electrons:
			if !ok {
				a.event(Error{
					Event: Event{
						Message: "atom receiver closed",
						AtomID:  ID(atom),
					},
				})
				return
			}

			a.event(Event{
				Message:     "new instance of electron",
				ElectronID:  inst.electron.ID,
				AtomID:      ID(atom),
				ConductorID: ID(inst.conductor),
			})

			// TODO: implement the processing push
			// TODO: after the processing has started
			// push to instances channel for monitoring
			// by the sampler so that this second can
			// focus on starting additional instances
			// rather than on individually bonded
			// instances

			// Initialize a new copy of the atom
			newAtom := reflect.New(
				reflect.TypeOf(atom).Elem(),
			)

			// ok is not checked here because this should
			// never fail since the originating data item
			// is what created this
			atom, _ := newAtom.Interface().(Atom)

			a.exec(inst, atom)
		}
	}
}

func (a *atomizer) exec(
	inst instance,
	atom Atom,
) {

	// bond the new atom instantiation to the electron instance
	if err := inst.bond(atom); err != nil {
		a.event(Error{
			Event: Event{
				Message:     "error while bonding",
				AtomID:      ID(atom),
				ElectronID:  inst.electron.ID,
				ConductorID: ID(inst.conductor),
			},
			Internal: err,
		})
		return
	}

	// Execute the instance after it's been
	// picked up for monitoring
	err := inst.execute(a.ctx)
	if err != nil {
		if inst.properties.Error != nil {
			inst.properties.Error = simple(
				"execution error",
				simple(err.Error(),
					simple(
						"instance error",
						inst.properties.Error,
					),
				),
			)
		} else {
			inst.properties.Error = err
		}

		if inst.conductor != nil {

			a.event(
				inst.conductor.Complete(a.ctx, *inst.properties),
			)
		}

		a.event(Error{
			Internal: inst.properties.Error,
			Event: Event{
				Message:    "error executing atom",
				AtomID:     ID(atom),
				ElectronID: inst.electron.ID,
			},
		})
	}
}

func (a *atomizer) distribute() {

	for {
		select {
		case <-a.ctx.Done():
			return
		case inst, ok := <-a.electrons:
			if !ok {
				a.event(Error{
					Event: Event{
						Message:     "dist channel closed",
						AtomID:      inst.electron.AtomID,
						ElectronID:  inst.electron.ID,
						ConductorID: ID(inst.conductor),
					},
				})

				return
			}

			a.atomsMu.RLock()
			achan, ok := a.atoms[inst.electron.AtomID]
			a.atomsMu.RUnlock()

			if !ok {
				// TODO: figure out what to do here
				// since the atom doesn't exist in
				// the registry

				a.event(Error{
					Event: Event{
						Message:    "not registered",
						AtomID:     inst.electron.AtomID,
						ElectronID: inst.electron.ID,
					},
				})
				continue
			}

			a.event(Event{
				Message:     "pushing electron to atom",
				ElectronID:  inst.electron.ID,
				AtomID:      inst.electron.AtomID,
				ConductorID: ID(inst.conductor),
			})

			select {
			case <-a.ctx.Done():
				return
			case achan <- inst:
				a.event(Event{
					Message:     "pushed electron to atom",
					ElectronID:  inst.electron.ID,
					AtomID:      inst.electron.AtomID,
					ConductorID: ID(inst.conductor),
				})
			}
		}
	}
}
