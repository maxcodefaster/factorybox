package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// This experiment gate lives in the controller, outside all guest snapshots.
// It demonstrates idle-agent admission only; it does not stop active inference.
type assignmentGate struct {
	leases   map[string]string
	admitted map[string]string
}

func newAssignmentGate() *assignmentGate {
	return &assignmentGate{leases: map[string]string{}, admitted: map[string]string{}}
}

func (g *assignmentGate) register(id string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	g.leases[id] = token
	delete(g.admitted, id)
	return token, nil
}

func (g *assignmentGate) admit(id, token, commandID string) (bool, error) {
	if token == "" || g.leases[id] != token {
		return false, fmt.Errorf("stale or absent assignment")
	}
	if commandID == "" {
		return false, fmt.Errorf("assignment identity required")
	}
	if previous := g.admitted[id]; previous != "" {
		if previous == commandID {
			return false, nil
		}
		return false, fmt.Errorf("another assignment already admitted")
	}
	g.admitted[id] = commandID
	return true, nil
}

func controllerProof() error {
	g := newAssignmentGate()
	parent, err := g.register("original-full")
	if err != nil {
		return err
	}
	for n := range 2 {
		prefix := os.Getenv("LAB_PROOF_PREFIX")
		if prefix == "" {
			prefix = "full"
		}
		id := fmt.Sprintf("%s-copy-%d", prefix, n)
		lease, err := g.register(id)
		if err != nil {
			return err
		}
		start := time.Now()
		if allowed, stale := g.admit(id, parent, "old-parent-task"); allowed || stale == nil {
			return fmt.Errorf("copied parent assignment accepted")
		}
		base := "import {OpenCode} from '/opt/opencode/node_modules/@opencode/client/dist/promise/index.js';const c=OpenCode.make({baseUrl:'http://127.0.0.1:4096',headers:{Authorization:'Basic '+Buffer.from('opencode:local-experiment-only').toString('base64')}});"
		out, err := guest(id, "/opt/opencode/bun", "-e", base+"const active=await c.session.active();if(Object.keys(active).length)throw Error('Agent already active');console.log(JSON.stringify({server:await c.server.info(),active}));")
		record(id+"-waiting-for-assignment", start, out, err)
		if err != nil {
			return err
		}
		commandID := "new-task-" + id
		allowed, err := g.admit(id, lease, commandID)
		if err != nil || !allowed {
			return fmt.Errorf("new assignment rejected: %v", err)
		}
		start = time.Now()
		out, err = guest(id, "/opt/opencode/bun", "-e", base+"const session=await c.session.create({title:'"+commandID+"',location:{directory:'/workspace/fixture'}});console.log(JSON.stringify({id:session.id,title:session.title}));")
		record(id+"-assigned-native-session", start, out, err)
		if err != nil {
			return err
		}
		if allowed, err = g.admit(id, lease, commandID); err != nil || allowed {
			return fmt.Errorf("duplicate assignment was not suppressed")
		}
		record(id+"-assignment-replay", time.Now(), "one native session create admitted; duplicate delivery suppressed", nil)
	}
	return nil
}
