// Copyright 2020 ConsenSys Software Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by gnark DO NOT EDIT

package cs

import (
	"github.com/fxamacker/cbor/v2"
	"io"
	"time"

	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	csolver "github.com/consensys/gnark/constraint/solver"
	"github.com/consensys/gnark/internal/backend/ioutils"
	"github.com/consensys/gnark/logger"
	"reflect"

	"github.com/consensys/gnark-crypto/ecc"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
)

type R1CS = system
type SparseR1CS = system

// system is a curved-typed constraint.System with a concrete coefficient table (fr.Element)
type system struct {
	constraint.System
	CoeffTable
	field
}

func NewR1CS(capacity int) *R1CS {
	return newSystem(capacity, constraint.SystemR1CS)
}

func NewSparseR1CS(capacity int) *SparseR1CS {
	return newSystem(capacity, constraint.SystemSparseR1CS)
}

func newSystem(capacity int, t constraint.SystemType) *system {
	return &system{
		System:     constraint.NewSystem(fr.Modulus(), capacity, t),
		CoeffTable: newCoeffTable(capacity / 10),
	}
}

// Solve solves the constraint system with provided witness.
// If it's a R1CS returns R1CSSolution
// If it's a SparseR1CS returns SparseR1CSSolution
func (cs *system) Solve(witness witness.Witness, opts ...csolver.Option) (any, error) {
	log := logger.Logger().With().Int("nbConstraints", cs.GetNbConstraints()).Logger()
	start := time.Now()

	v := witness.Vector().(fr.Vector)

	// init the solver
	solver, err := newSolver(cs, v, opts...)
	if err != nil {
		log.Err(err).Send()
		return nil, err
	}

	// defer log printing once all solver.values are computed
	// (or sooner, if a constraint is not satisfied)
	defer solver.printLogs(cs.Logs)

	// run it.
	if err := solver.run(); err != nil {
		log.Err(err).Send()
		return nil, err
	}

	log.Debug().Dur("took", time.Since(start)).Msg("constraint system solver done")

	// format the solution
	// TODO @gbotrel revisit post-refactor
	if cs.Type == constraint.SystemR1CS {
		var res R1CSSolution
		res.W = solver.values
		res.A = solver.a
		res.B = solver.b
		res.C = solver.c
		return &res, nil
	} else {
		// sparse R1CS
		var res SparseR1CSSolution
		// query l, r, o in Lagrange basis, not blinded
		res.L, res.R, res.O = evaluateLROSmallDomain(cs, solver.values)

		return &res, nil
	}

}

// IsSolved
// Deprecated: use _, err := Solve(...) instead
func (cs *system) IsSolved(witness witness.Witness, opts ...csolver.Option) error {
	_, err := cs.Solve(witness, opts...)
	return err
}

// GetR1Cs return the list of R1C
func (cs *system) GetR1Cs() []constraint.R1C {
	toReturn := make([]constraint.R1C, 0, cs.GetNbConstraints())

	for _, inst := range cs.Instructions {
		blueprint := cs.Blueprints[inst.BlueprintID]
		if bc, ok := blueprint.(constraint.BlueprintR1C); ok {
			var r1c constraint.R1C
			bc.DecompressR1C(&r1c, inst.Unpack(&cs.System))
			toReturn = append(toReturn, r1c)
		} else {
			panic("not implemented")
		}
	}
	return toReturn
}

// GetNbCoefficients return the number of unique coefficients needed in the R1CS
func (cs *system) GetNbCoefficients() int {
	return len(cs.Coefficients)
}

// CurveID returns curve ID as defined in gnark-crypto
func (cs *system) CurveID() ecc.ID {
	return ecc.BLS12_381
}

// WriteTo encodes R1CS into provided io.Writer using cbor
func (cs *system) WriteTo(w io.Writer) (int64, error) {
	_w := ioutils.WriterCounter{W: w} // wraps writer to count the bytes written
	ts := getTagSet()
	enc, err := cbor.CoreDetEncOptions().EncModeWithTags(ts)
	if err != nil {
		return 0, err
	}
	encoder := enc.NewEncoder(&_w)

	// encode our object
	err = encoder.Encode(cs)
	return _w.N, err
}

// ReadFrom attempts to decode R1CS from io.Reader using cbor
func (cs *system) ReadFrom(r io.Reader) (int64, error) {
	ts := getTagSet()
	dm, err := cbor.DecOptions{
		MaxArrayElements: 134217728,
		MaxMapPairs:      134217728,
	}.DecModeWithTags(ts)

	if err != nil {
		return 0, err
	}
	decoder := dm.NewDecoder(r)

	// initialize coeff table
	cs.CoeffTable = newCoeffTable(0)

	if err := decoder.Decode(&cs); err != nil {
		return int64(decoder.NumBytesRead()), err
	}

	if err := cs.CheckSerializationHeader(); err != nil {
		return int64(decoder.NumBytesRead()), err
	}

	return int64(decoder.NumBytesRead()), nil
}

func (cs *system) GetCoefficient(i int) (r constraint.Element) {
	copy(r[:], cs.Coefficients[i][:])
	return
}

// GetSparseR1Cs return the list of SparseR1C
func (cs *system) GetSparseR1Cs() []constraint.SparseR1C {

	toReturn := make([]constraint.SparseR1C, 0, cs.GetNbConstraints())

	for _, inst := range cs.Instructions {
		blueprint := cs.Blueprints[inst.BlueprintID]
		if bc, ok := blueprint.(constraint.BlueprintSparseR1C); ok {
			var sparseR1C constraint.SparseR1C
			bc.DecompressSparseR1C(&sparseR1C, inst.Unpack(&cs.System))
			toReturn = append(toReturn, sparseR1C)
		} else {
			panic("not implemented")
		}
	}
	return toReturn
}

// evaluateLROSmallDomain extracts the solver l, r, o, and returns it in lagrange form.
// solver = [ public | secret | internal ]
// TODO @gbotrel refactor; this seems to be a small util function for plonk
func evaluateLROSmallDomain(cs *system, solution []fr.Element) ([]fr.Element, []fr.Element, []fr.Element) {

	//s := int(pk.Domain[0].Cardinality)
	s := cs.GetNbConstraints() + len(cs.Public) // len(spr.Public) is for the placeholder constraints
	s = int(ecc.NextPowerOfTwo(uint64(s)))

	var l, r, o []fr.Element
	l = make([]fr.Element, s)
	r = make([]fr.Element, s)
	o = make([]fr.Element, s)
	s0 := solution[0]

	for i := 0; i < len(cs.Public); i++ { // placeholders
		l[i] = solution[i]
		r[i] = s0
		o[i] = s0
	}
	offset := len(cs.Public)
	nbConstraints := cs.GetNbConstraints()

	var sparseR1C constraint.SparseR1C
	j := 0
	for _, inst := range cs.Instructions {
		blueprint := cs.Blueprints[inst.BlueprintID]
		if bc, ok := blueprint.(constraint.BlueprintSparseR1C); ok {
			bc.DecompressSparseR1C(&sparseR1C, inst.Unpack(&cs.System))

			l[offset+j] = solution[sparseR1C.XA]
			r[offset+j] = solution[sparseR1C.XB]
			o[offset+j] = solution[sparseR1C.XC]
			j++
		}
	}

	offset += nbConstraints

	for i := 0; i < s-offset; i++ { // offset to reach 2**n constraints (where the id of l,r,o is 0, so we assign solver[0])
		l[offset+i] = s0
		r[offset+i] = s0
		o[offset+i] = s0
	}

	return l, r, o

}

// R1CSSolution represent a valid assignment to all the variables in the constraint system.
// The vector W such that Aw o Bw - Cw = 0
type R1CSSolution struct {
	W       fr.Vector
	A, B, C fr.Vector
}

func (t *R1CSSolution) WriteTo(w io.Writer) (int64, error) {
	n, err := t.W.WriteTo(w)
	if err != nil {
		return n, err
	}
	a, err := t.A.WriteTo(w)
	n += a
	if err != nil {
		return n, err
	}
	a, err = t.B.WriteTo(w)
	n += a
	if err != nil {
		return n, err
	}
	a, err = t.C.WriteTo(w)
	n += a
	return n, err
}

func (t *R1CSSolution) ReadFrom(r io.Reader) (int64, error) {
	n, err := t.W.ReadFrom(r)
	if err != nil {
		return n, err
	}
	a, err := t.A.ReadFrom(r)
	a += n
	if err != nil {
		return n, err
	}
	a, err = t.B.ReadFrom(r)
	a += n
	if err != nil {
		return n, err
	}
	a, err = t.C.ReadFrom(r)
	n += a
	return n, err
}

// SparseR1CSSolution represent a valid assignment to all the variables in the constraint system.
type SparseR1CSSolution struct {
	L, R, O fr.Vector
}

func (t *SparseR1CSSolution) WriteTo(w io.Writer) (int64, error) {
	n, err := t.L.WriteTo(w)
	if err != nil {
		return n, err
	}
	a, err := t.R.WriteTo(w)
	n += a
	if err != nil {
		return n, err
	}
	a, err = t.O.WriteTo(w)
	n += a
	return n, err

}

func (t *SparseR1CSSolution) ReadFrom(r io.Reader) (int64, error) {
	n, err := t.L.ReadFrom(r)
	if err != nil {
		return n, err
	}
	a, err := t.R.ReadFrom(r)
	a += n
	if err != nil {
		return n, err
	}
	a, err = t.O.ReadFrom(r)
	a += n
	return n, err
}

func getTagSet() cbor.TagSet {
	// temporary for refactor
	ts := cbor.NewTagSet()
	// https://www.iana.org/assignments/cbor-tags/cbor-tags.xhtml
	// 65536-15309735 Unassigned
	tagNum := uint64(5309735)
	addType := func(t reflect.Type) {
		if err := ts.Add(
			cbor.TagOptions{EncTag: cbor.EncTagRequired, DecTag: cbor.DecTagRequired},
			t,
			tagNum,
		); err != nil {
			panic(err)
		}
		tagNum++
	}

	addType(reflect.TypeOf(constraint.BlueprintGenericHint{}))
	addType(reflect.TypeOf(constraint.BlueprintGenericR1C{}))
	addType(reflect.TypeOf(constraint.BlueprintGenericSparseR1C{}))
	addType(reflect.TypeOf(constraint.BlueprintSparseR1CAdd{}))
	addType(reflect.TypeOf(constraint.BlueprintSparseR1CMul{}))
	addType(reflect.TypeOf(constraint.BlueprintSparseR1CBool{}))

	return ts
}
