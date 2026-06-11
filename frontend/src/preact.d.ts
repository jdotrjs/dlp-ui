// Preact JSX typing shim.
//
// In preact 10.29 with TypeScript's strict JSX checking, a component whose
// declared call-signature return type is `ComponentChildren` (which includes
// `undefined`) — notably the Context `Provider` returned by createContext — is
// rejected with TS2786 ("cannot be used as a JSX component … 'undefined' is not
// assignable to type 'Element | null'"). Our own function components type-check
// fine because TS infers their concrete JSX return; only the library-typed
// Provider trips the check.
//
// Overriding FunctionComponent's call-signature return type to a proper JSX
// element (VNode | null) makes the Provider — and any component typed via the
// FunctionComponent interface — a valid JSX element. This narrows the library
// type rather than loosening JSX.Element itself.
import 'preact';

declare module 'preact' {
  interface FunctionComponent<P = {}> {
    (props: RenderableProps<P>, context?: any): VNode<any> | null;
  }
}
