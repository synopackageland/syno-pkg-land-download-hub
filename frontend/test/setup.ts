import {vi} from 'vitest'
// jsdom has no CSS.escape; SectionTabs uses the browser API for tab selectors.
import 'css.escape'
vi.stubGlobal('ResizeObserver', class {observe() {} unobserve() {} disconnect() {}})
vi.stubGlobal('matchMedia', () => ({matches:false,addEventListener(){},removeEventListener(){}}))
Element.prototype.scrollIntoView = () => {}
// jsdom's CSSStyleDeclaration is not iterable; AppRoot spreads element.style.
if(!(Symbol.iterator in CSSStyleDeclaration.prototype)){
 Object.defineProperty(CSSStyleDeclaration.prototype,Symbol.iterator,{configurable:true,value:function*(this:CSSStyleDeclaration){for(let i=0;i<this.length;i++)yield this.item(i)}})
}
// Select opens on pointerdown and asks for pointer capture, which jsdom lacks.
Element.prototype.hasPointerCapture = () => false
Element.prototype.setPointerCapture = () => {}
Element.prototype.releasePointerCapture = () => {}
