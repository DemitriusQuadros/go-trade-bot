package volume

import (
	"go-trade-bot/app/entities"
	"log"
)

type VolumeProcessor struct {
	strategy entities.Strategy
}

func NewVolumeProcessor(s entities.Strategy) VolumeProcessor {
	return VolumeProcessor{
		strategy: s,
	}
}

func (p VolumeProcessor) Execute() error {
	// Implement the logic to execute the volume strategy
	// This is a placeholder for the actual implementation
	log.Printf("Executing Volume Strategy: %s", p.strategy.Name)
	return nil
}

//
