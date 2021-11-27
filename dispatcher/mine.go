package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/pubsub"
	"github.com/google/go-github/v40/github"
	"golang.org/x/oauth2"

	"github.com/scylladb/go-set/strset"
	//"zntr.io/typogenerator"
	//"zntr.io/typogenerator/mapping"
	//"zntr.io/typogenerator/strategy"
)

type ForkFinder struct {
	Ctx         context.Context
	Owner       string
	Name        string
	Client      *github.Client
	PubsubTopic *pubsub.Topic
	Cache       *strset.Set
}

func NewForkFinder(ctx context.Context, payload *JobPayload) (*ForkFinder, error) {
	log.Println("Creating authenticated GitHub client")
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: payload.Token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	projectID := os.Getenv("GOOGLE_PROJECT_ID")
	topicID := os.Getenv("ANALYSIS_QUEUE")

	log.Println("Creating pubsub client and topic")
	pubsubClient, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub.NewClient: %v", err)
	}
	defer pubsubClient.Close()
	topic := pubsubClient.Topic(topicID)

	return &ForkFinder{
		Ctx:         ctx,
		Owner:       payload.Owner,
		Name:        payload.Repo,
		PubsubTopic: topic,
		Client:      client,
		Cache:       strset.New(),
	}, nil
}

// With an instantiated `ForkFinder`, dispatch our API and fuzzing heuristics asynchronously,
// caching repository names that are found.
func (f *ForkFinder) FindAndDispatch() error {
	if err := f.RecoverValidForks(); err != nil {
		return err
	}
	return nil
}

/*
// Given a repository, fuzz the owner and repo names to detect for "unlinked" forks.
// TODO: limit strategies to limit API invocations
func (f *ForkFinder) FuzzRepo() error {
	strategies := []strategy.Strategy{
		strategy.Addition,
		strategy.BitSquatting,
		strategy.Homoglyph,
		strategy.Omission,
		strategy.Repetition,
		strategy.Transposition,
		strategy.Prefix,
		strategy.Hyphenation,
		strategy.VowelSwap,
		strategy.Replace(mapping.English),
		strategy.DoubleHit(mapping.English),
		strategy.Similar(mapping.English),
	}

	// check for
	results, err := typogenerator.Fuzz(f.Owner, strategies...)
	if err != nil {
		return err
	}
	return nil
}
*/

// Given a repository name, create an authenticated client and recover
// all valid forks, including all subforks for that repo.
func (f *ForkFinder) RecoverValidForks() error {
	log.Println("Listing forks for repository")

	opts := github.RepositoryListForksOptions{
		Sort: "newest",
	}

	forks, _, err := f.Client.Repositories.ListForks(f.Ctx, f.Owner, f.Name, &opts)
	if err != nil {
		return err
	}

	// do a depth-first search, and traverse each fork
	log.Println("Iterating and sanity checking recovered forks")
	for _, fork := range forks {
		name := fork.GetFullName()

		// sanity check fork for existence
		if fork.GetPrivate() {
			log.Printf("Skipping %s, is a private repo", name)
			continue
		}

		ok, err := DirtyExistenceCheck(name)
		if err != nil {
			return err
		}
		if !ok {
			log.Printf("Skipping %s, may be private or deleted", name)
			continue
		}

		// traverse further if there are subforks
		count := fork.GetForksCount()
		if count != 0 {
			// TODO
		}

		log.Printf("Publishing fork %s for analysis", name)
		_ = f.PubsubTopic.Publish(f.Ctx, &pubsub.Message{
			Data: []byte(name),
		})
	}
	return nil
}