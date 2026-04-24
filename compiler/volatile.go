package compiler

import "moxie/typecheck"

func (b *builder) createVolatileLoad() {
	b.createFunctionStart(true)
	p0 := b.mxFn.Params[0]
	addr := b.getValueTC(p0, getPosTC(p0))
	b.createNilCheckTC(p0, addr, "deref")
	valType := b.getLLVMTypeTC(p0.Type().(*typecheck.Pointer).Elem())
	val := b.CreateLoad(valType, addr, "")
	val.SetVolatile(true)
	b.CreateRet(val)
}

func (b *builder) createVolatileStore() {
	b.createFunctionStart(true)
	p0 := b.mxFn.Params[0]
	p1 := b.mxFn.Params[1]
	addr := b.getValueTC(p0, getPosTC(p0))
	val := b.getValueTC(p1, getPosTC(p1))
	b.createNilCheckTC(p0, addr, "deref")
	store := b.CreateStore(val, addr)
	store.SetVolatile(true)
	b.CreateRetVoid()
}
