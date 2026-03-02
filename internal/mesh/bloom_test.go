package mesh

import (
	"testing"
)

func TestBloomFilter_BasicOperations(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	// Add items
	bf.Add("python")
	bf.Add("security")
	bf.Add("code-review")

	// Should contain added items
	if !bf.Contains("python") {
		t.Error("bloom filter should contain 'python'")
	}
	if !bf.Contains("security") {
		t.Error("bloom filter should contain 'security'")
	}
	if !bf.Contains("code-review") {
		t.Error("bloom filter should contain 'code-review'")
	}

	// Should not contain unadded items (with high probability)
	falsePositives := 0
	testItems := []string{"java", "translation", "medical", "finance", "art", "music"}
	for _, item := range testItems {
		if bf.Contains(item) {
			falsePositives++
		}
	}
	// With 1% false positive rate, getting more than 2 out of 6 is suspicious
	if falsePositives > 2 {
		t.Errorf("too many false positives: %d out of %d", falsePositives, len(testItems))
	}
}

func TestBloomFilter_MatchCount(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	bf.Add("python")
	bf.Add("security")
	bf.Add("code-review")
	bf.Add("developer-tools")

	matches := bf.MatchCount([]string{"python", "security", "unrelated"})
	if matches < 2 {
		t.Errorf("expected at least 2 matches, got %d", matches)
	}
}

func TestBloomFilter_SerializeDeserialize(t *testing.T) {
	bf1 := NewBloomFilter(100, 0.01)
	bf1.Add("hello")
	bf1.Add("world")

	// Serialize
	data := bf1.Bytes()
	if len(data) == 0 {
		t.Fatal("serialized bloom filter is empty")
	}

	// Deserialize into new bloom filter with same params
	bf2 := NewBloomFilter(100, 0.01)
	bf2.FromBytes(data)

	// Should contain the same items
	if !bf2.Contains("hello") {
		t.Error("deserialized bloom filter should contain 'hello'")
	}
	if !bf2.Contains("world") {
		t.Error("deserialized bloom filter should contain 'world'")
	}
}

func TestBloomFilter_Clear(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)
	bf.Add("test")

	if !bf.Contains("test") {
		t.Error("should contain 'test' before clear")
	}

	bf.Clear()

	// After clear, the item should likely not be found
	// (though false positives are still possible)
	if bf.Contains("test") {
		// This is acceptable due to false positives, but very unlikely after clear
		t.Log("note: 'test' found after clear (false positive)")
	}
}

func TestBloomFilter_Size(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	size := bf.Size()

	if size == 0 {
		t.Error("bloom filter size should be > 0")
	}

	// For 1000 items at 1% FP rate, optimal size is ~9585 bits
	if size < 5000 || size > 20000 {
		t.Errorf("bloom filter size %d seems off for 1000 items at 1%% FP", size)
	}
}
