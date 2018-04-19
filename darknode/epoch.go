package darknode

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/republicprotocol/republic-go/dispatch"
	"github.com/republicprotocol/republic-go/ethereum/contracts"
	"github.com/republicprotocol/republic-go/identity"
	"github.com/republicprotocol/republic-go/rpc"
	"github.com/republicprotocol/republic-go/smpc"
)

// RunEpochProcess until the done channel is closed. Epochs define the Pools in
// which Darknodes cooperate to match orders by receiving order.Fragments from
// traders, performing secure multi-party computations. The EpochProcess will
// register with the EpochSwitcher by message passing an EpochRoute.
func RunEpochProcess(done <-chan struct{}, epochRoutes chan<- EpochRoute, id identity.ID, darkOcean DarkOcean, router Router) (<-chan smpc.Delta, <-chan error) {
	deltas := make(chan smpc.Delta)
	errs := make(chan error)

	go func() {
		defer close(deltas)

		pool, err := darkOcean.Pool(id)
		if err != nil {
			errs <- err
			return
		}

		// Open connections with all Darknodes in the Pool
		senders := map[identity.Address]chan<- *rpc.Computation{}
		receivers := map[identity.Address]<-chan *rpc.Computation{}
		errors := map[identity.Address]<-chan error{}
		for _, address := range pool.Addresses() {
			sender := make(chan *rpc.Computation)
			defer close(sender)

			senders[address] = sender
			receivers[address], errors[address] = router.Compute(darkOcean.Epoch, address, sender)
		}

		n := pool.Size()
		k := (pool.Size() + 1) * 2 / 3
		smpcer := smpc.NewComputer(id, n, k)

		// Run secure multi-party computer
		deltaFragments := make(chan smpc.DeltaFragment)
		close(deltaFragments)
		deltaFragmentsComputed, deltasComputed := smpcer.ComputeOrderMatches(done, router.OpenOrders(darkOcean.Epoch), deltaFragments)

		<-dispatch.Dispatch(func() {
			// Receive smpc.DeltaFragments from other Darknodes in the Pool
			for _, receiver := range receivers {
				go func(receiver <-chan *rpc.Computation) {
					select {
					case <-done:
						return

					case computation, ok := <-receiver:
						if !ok {
							return
						}
						if computation.DeltaFragment != nil {
							deltaFragment, err := rpc.UnmarshalDeltaFragment(computation.DeltaFragment)
							if err != nil {
								// ???
							}
							deltaFragments <- deltaFragment
						}
					}
				}(receiver)
			}
		}, func() {
			// Broadcast computed smpc.DeltaFragments to other Darknodes in the
			// Pool
			for {
				select {
				case <-done:
					return

				case deltaFragment, ok := <-deltaFragmentsComputed:
					if !ok {
						return
					}
					computation := &rpc.Computation{DeltaFragment: rpc.MarshalDeltaFragment(&deltaFragment)}
					for _, sender := range senders {
						ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
						select {
						case <-done:
							cancel()
							return
						case <-ctx.Done():
							cancel()
							continue
						case sender <- computation:
						}
						cancel()
					}
				}
			}
		}, func() {
			// Output smpc.Deltas that have been computed
			dispatch.Pipe(done, deltasComputed, deltas)
		})
	}()

	return deltas, errs
}

// RunEpochWatcher until the done channel is closed. An EpochWatcher will watch
// for changes to the DarknodeRegistry epoch. Returns a read-only channel that
// can be used to read epochs as they change.
func RunEpochWatcher(done <-chan struct{}, darknodeRegistry contracts.DarkNodeRegistry) (<-chan contracts.Epoch, <-chan error) {
	changes := make(chan contracts.Epoch)
	errs := make(chan error, 1)

	go func() {
		defer close(changes)
		defer close(errs)

		minimumEpochInterval, err := darknodeRegistry.MinimumEpochInterval()
		if err != nil {
			errs <- fmt.Errorf("cannot get minimum epoch interval: %v", err)
			return
		}

		currentEpoch, err := darknodeRegistry.CurrentEpoch()
		if err != nil {
			errs <- fmt.Errorf("cannot get current epoch: %v", err)
			return
		}

		for {
			// Signal that the epoch has changed
			select {
			case <-done:
				return
			case changes <- currentEpoch:
			}

			// Sleep until the next epoch
			nextEpochTime := currentEpoch.Timestamp.Add(&minimumEpochInterval)
			nextEpochTimeUnix, err := nextEpochTime.ToUint()
			if err != nil {
				errs <- fmt.Errorf("cannot convert epoch timestamp to unix timestamp: %v", err)
				return
			}
			delay := time.Duration(int64(nextEpochTimeUnix)-time.Now().Unix()) * time.Second
			time.Sleep(delay)

			// Spin-lock until the new epoch is detected or until the done
			// channel is closed
			for {

				select {
				case <-done:
					return
				default:
				}

				nextEpoch, err := darknodeRegistry.CurrentEpoch()
				if err != nil {
					errs <- fmt.Errorf("cannot get next epoch: %v", err)
					return
				}
				if !bytes.Equal(currentEpoch.Blockhash[:], nextEpoch.Blockhash[:]) {
					currentEpoch = nextEpoch
					break
				}
			}
		}
	}()

	return changes, errs
}