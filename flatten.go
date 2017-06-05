package lazyexp

import (
	"time"
)

// ID uniquely identifies a FlatNode in FlattenedNodes
type ID int

// RootID is the canonical ID of the root node, i.e. the node passed to Flatten()
const RootID ID = 0

// NoChild is a special ID used to signify the absence of a child
const NoChild ID = -1

// FlattenedNodes is a non-recursive representation of a Node graph, returned by Flatten().
type FlattenedNodes map[ID]FlatNode

// FlatDependency describes a dependency between two FlatNode values.
type FlatDependency struct {
	ID  ID
	Err error
	// TODO: include completionStrategy
}

// FlatNode is a non-recursive Node representation
type FlatNode struct {
	ID             ID
	Description    string
	Child          ID
	Dependencies   []FlatDependency
	Evaluated      bool
	Err            error
	FetchStartTime time.Time // Zero if not yet started to fetch
	FetchEndTime   time.Time // Zero if not yet fetched
}

// Flatten transform a Node Graph into a non-recursive representation suitable for serialization.
func Flatten(node Node) FlattenedNodes {
	flattener := nodeFlattener{
		result:  FlattenedNodes{},
		visited: map[Node]ID{},
		nextID:  RootID,
	}
	node.flatten(&flattener)
	return flattener.result
}

type nodeFlattener struct {
	result  FlattenedNodes
	visited map[Node]ID
	nextID  ID
}

func (nf *nodeFlattener) getID(node Node) (id ID, visited bool) {
	id, visited = nf.visited[node]
	if !visited {
		id = nf.nextID
		nf.nextID++
		nf.visited[node] = id
	}
	return
}
