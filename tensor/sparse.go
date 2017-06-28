package tensor

import (
	"reflect"
	"unsafe"

	"github.com/pkg/errors"
	"log"
	"sort"
)

var (
	_ Sparse = &CS{}
)

type Sparse interface {
	Tensor
	Densor
	NonZeroes() int     // NonZeroes returns the number of nonzero values
	Iterator() Iterator // get an iterator
}

// coo is an internal representation of the Coordinate type sparse matrix. It's not exported because you probably shouldn't be using it
type coo struct {
	o DataOrder
	xs, ys []int
	data array
}

func (c *coo) Len() int {return c.data.l}
func (c *coo) Less(i, j int) bool {
	if c.o.isColMajor() {
		return c.colMajorLess(i,j)
	}
	return c.rowMajorLess(i, j)
}
func (c *coo) Swap(i, j int) {
	c.xs[i], c.xs[j] = c.xs[j], c.xs[i]
	c.ys[i], c.ys[j] = c.ys[j], c.ys[i]
	c.data.swap(i,j)	
}

func (c *coo) colMajorLess(i, j int) bool {
	if c.ys[i] < c.ys[j] {
		return true
	}
	if c.ys[i] == c.ys[j]{
		// check xs
		if c.xs[i] <= c.xs[j] {
			return true
		}
	}
	return false
}

func (c *coo) rowMajorLess(i, j int) bool{
	if c.xs[i] < c.xs[j] {
		return true
	}

	if c.xs[i] == c.xs[j] {
		// check ys
		if c.ys[i] < c.ys[j] {
			return true
		}
	}
	return false
}

// CS is a compressed sparse data structure. It can be used to represent both CSC and CSR sparse matrices.
// Refer to the individual creation functions for more information.
type CS struct {
	s Shape
	o DataOrder

	indices []int
	indptr  []int

	array
	e Engine
}

// NewCSR creates a new Compressed Sparse Row matrix
func NewCSR(indices, indptr []int, data interface{}, opts ...ConsOpt) *CS {
	t := new(CS)
	t.indices = indices
	t.indptr = indptr
	t.array = arrayFromSlice(data)
	t.o = NonContiguous

	for _, opt := range opts {
		opt(t)
	}
	return t
}

func NewCSC(indices, indptr []int, data interface{}, opts ...ConsOpt) *CS {
	t := new(CS)
	t.indices = indices
	t.indptr = indptr
	t.array = arrayFromSlice(data)
	t.o = MakeDataOrder(ColMajor, NonContiguous)

	for _, opt := range opts {
		opt(t)
	}
	return t
}

func CSRFromCoord(rows, cols []int, shape Shape, data interface{}) *CS {
	t := new(CS)
	t.s = shape
	t.o = NonContiguous
	t.array = arrayFromSlice(data)

	r := shape[0]
	indptr := make([]int, r+1)

	var i, j, tmp int
	for i = 1; i < r+1; i++ {
		for j = tmp; j < len(rows) && rows[j] < i; j++ {

		}
		tmp = j
		indptr[i] = j
	}
	t.indices = cols
	t.indptr = indptr
	return t
}

func CSCFromCoord(xs, ys []int, shape Shape, data interface{}) *CS {
	t := new(CS)
	t.s = shape
	t.o = MakeDataOrder(NonContiguous, ColMajor)
	t.array = arrayFromSlice(data)

	// coord matrix 
	cm := &coo{t.o, xs, ys, t.array}
	sort.Sort(cm)

	c := shape[1]
	indptr := make([]int, c+1)

	var i, j, tmp int
	for i = 1; i < c+1; i++ {
		for j = tmp; j < len(ys) && ys[j] < i; j++ {

		}
		tmp = j
		indptr[i] = j
	}
	t.indices = xs
	t.indptr = indptr
	return t
}

func (t *CS) Shape() Shape   { return t.s }
func (t *CS) Strides() []int { return nil }
func (t *CS) Dtype() Dtype   { return t.t }
func (t *CS) Dims() int      { return 2 }
func (t *CS) Size() int      { return t.s.TotalSize() }
func (t *CS) DataSize() int  { return t.l }
func (t *CS) Engine() Engine { return t.e }

func (t *CS) At(coord ...int) (interface{}, error) {
	if len(coord) != t.Dims() {
		return nil, errors.Errorf("Expected coordinates to be of %d-dimensions. Got %v instead", t.Dims(), coord)
	}
	if i, ok := t.at(coord...); ok {
		return t.Get(i), nil
	}

	return reflect.Zero(t.t.Type), nil
}

func (t *CS) SetAt(v interface{}, coord ...int) error {
	if i, ok := t.at(coord...); ok {
		t.Set(i, v)
		return nil
	}
	return errors.New("Cannot set value in a compressed sparse matrix")
}

func (t *CS) Reshape(...int) error { return errors.New("compressed sparse matrix cannot be reshaped") }

func (t *CS) T(axes ...int) error {
	if len(axes) != t.Dims() && len(axes) != 0 {
		return errors.Errorf("Cannot transpose along axes %v", axes)
	}
	// toggle t.order
	// TODO
	return errors.Errorf(methodNYI, "T")
}

func (t *CS) UT() {}

func (t *CS) Transpose() {}

func (t *CS) Slice(...Slice) (Tensor, error) {
	return nil, errors.New("compressed sparse matrix cannot be sliced")
}

func (t *CS) Apply(fn interface{}, opts ...FuncOpt) (Tensor, error) {
	return nil, errors.Errorf(methodNYI, "Apply")
}

func (t *CS) Eq(other interface{}) bool {
	if ot, ok := other.(*CS); ok {
		if t == ot {
			return true
		}

		if len(ot.indices) != len(t.indices) {
			return false
		}
		if len(ot.indptr) != len(t.indptr) {
			return false
		}
		if !t.s.Eq(ot.s) {
			return false
		}
		if ot.o != t.o {
			return false
		}
		for i, ind := range t.indices {
			if ot.indices[i] != ind {
				return false
			}
		}
		for i, ind := range t.indptr {
			if ot.indptr[i] != ind {
				return false
			}
		}
		return t.array.Eq(ot.array)
	}
	return false
}

func (t *CS) Clone() interface{} {
	retVal := new(CS)
	retVal.s = t.s.Clone()
	retVal.o = t.o
	retVal.indices = make([]int, len(t.indices))
	retVal.indptr = make([]int, len(t.indptr))
	copy(retVal.indices, t.indices)
	copy(retVal.indptr, t.indptr)
	retVal.array = makeArray(t.t, t.l)
	copyArray(retVal.array, t.array)
	retVal.e = t.e
	return retVal
}

func (t *CS) IsScalar() bool           { return false }
func (t *CS) ScalarValue() interface{} { panic("Sparse Matrices cannot represent Scalar Values") }
func (t *CS) IsView() bool             { return false }
func (t *CS) Materialize() Tensor      { return t.Dense() }

func (t *CS) MemSize() uintptr        { return uintptr(calcMemSize(t.t, t.l)) }
func (t *CS) Uintptr() uintptr        { return uintptr(t.ptr) }
func (t *CS) Pointer() unsafe.Pointer { return t.ptr }

func (t *CS) NonZeroes() int     { return t.l }
func (t *CS) Iterator() Iterator { return nil } // not yet created

func (t *CS) at(coord ...int) (int, bool) {
	var r, c int
	if t.o.isColMajor() {
		r = coord[1]
		c = coord[0]
	} else {
		r = coord[0]
		c = coord[1]
	}

	for i := t.indptr[r]; i < t.indptr[r+1]; i++ {
		if t.indices[i] == c {
			return i, true
		}
	}
	return -1, false
}

func (t *CS) Dense() *Dense {
	// if t.e != nil , use engine instead
	d := recycledDense(t.t, t.Shape().Clone())

	if t.o.isColMajor() {
		for i := 0; i < len(t.indptr)-1; i++ {
			for j := t.indptr[i]; j < t.indptr[i+1]; j++ {
				log.Printf("Cord: %v %v: j: %d", i, t.indices[j], j)
				d.SetAt(t.Get(j), t.indices[j], i)
			}
		}
	} else {
		for i := 0; i < len(t.indptr)-1; i++ {
			for j := t.indptr[i]; j < t.indptr[i+1]; j++ {
				d.SetAt(t.Get(j), i, t.indices[j])
			}
		}
	}
	return d
}
