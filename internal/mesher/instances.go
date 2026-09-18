package mesher

func (b *builder) instanceAt(p [3]int) instance {
	if inst := b.instances[p]; inst != nil {
		return *inst
	}
	return instance{}
}
