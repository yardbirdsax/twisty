package osmconv

import "testing"

func TestObjectLess(t *testing.T) {
	tests := []struct {
		name string
		a, b Object
		want bool
	}{
		{
			name: "node before way",
			a:    Object{Type: NodeType, ID: 100},
			b:    Object{Type: WayType, ID: 1},
			want: true,
		},
		{
			name: "way before relation",
			a:    Object{Type: WayType, ID: 100},
			b:    Object{Type: RelationType, ID: 1},
			want: true,
		},
		{
			name: "node not less than node with smaller id",
			a:    Object{Type: NodeType, ID: 10},
			b:    Object{Type: NodeType, ID: 5},
			want: false,
		},
		{
			name: "same type sorted by id ascending",
			a:    Object{Type: WayType, ID: 5},
			b:    Object{Type: WayType, ID: 10},
			want: true,
		},
		{
			name: "equal objects are not less",
			a:    Object{Type: NodeType, ID: 1},
			b:    Object{Type: NodeType, ID: 1},
			want: false,
		},
		{
			name: "relation not less than node",
			a:    Object{Type: RelationType, ID: 1},
			b:    Object{Type: NodeType, ID: 1},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ObjectLess(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("ObjectLess(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestObjectTypeOrdering(t *testing.T) {
	if NodeType >= WayType {
		t.Errorf("NodeType (%d) should be less than WayType (%d)", NodeType, WayType)
	}
	if WayType >= RelationType {
		t.Errorf("WayType (%d) should be less than RelationType (%d)", WayType, RelationType)
	}
}
