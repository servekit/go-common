package lifecycle

import (
	"fmt"
	"sync/atomic"
)

// ModuleClaim enforces "one live module instance per process" for a
// provider's Connect. Claim fails while a previous instance is still
// active; Wrap releases the claim when the Manager stops that instance,
// so build-after-Stop stays legal (tests, graceful rebuild). Direct
// NewModule use is unguarded by design — only the composition path
// (Connect) is checked.
//
// Each provider holds one package-level claim:
//
//	var moduleClaim lifecycle.ModuleClaim
//
//	func Connect(cfg ConnectConfig, mgr *lifecycle.Manager) (Service, *Handler, error) {
//	    ...
//	    case "module", "":
//	        if err := moduleClaim.Claim("gid-service"); err != nil {
//	            return nil, nil, err
//	        }
//	        hdl, err := NewModule(cfg.Config, cfg.Opts...)
//	        if err != nil {
//	            moduleClaim.Release() // construction failed; free the slot
//	            return nil, nil, fmt.Errorf("gid-service: %w", err)
//	        }
//	        mgr.Add("gid-service", moduleClaim.Wrap(hdl))
//	        return hdl, hdl, nil
//	    }
//	}
type ModuleClaim struct {
	active atomic.Bool
}

// Claim reserves the process's single module slot for this provider. It
// returns an error when another instance built through Connect is still
// active — the composition root should share that instance via the
// provider's With*Handler option instead of building a second one.
func (m *ModuleClaim) Claim(name string) error {
	if m.active.Swap(true) {
		return fmt.Errorf("%s: another module instance is already active in this process — share the existing Handler via this provider's With*Handler option instead of building a second one, or stop the first instance first", name)
	}
	return nil
}

// Release frees the claim. Used when construction fails after a
// successful Claim, and by Wrap when the Manager stops the instance.
func (m *ModuleClaim) Release() { m.active.Store(false) }

// Wrap delegates Start/Stop to svc and releases the claim on Stop, so
// registering the wrapped value with Manager.Add ties the claim's
// lifetime to the instance's lifecycle.
func (m *ModuleClaim) Wrap(svc Service) Service {
	return &claimedModule{Service: svc, claim: m}
}

// claimedModule couples a service's Stop with releasing its ModuleClaim.
type claimedModule struct {
	Service
	claim *ModuleClaim
}

// Stop stops the underlying service and releases the claim regardless of
// the stop outcome — a stopping instance must not hold the slot.
func (c *claimedModule) Stop() error {
	err := c.Service.Stop()
	c.claim.Release()
	return err
}
