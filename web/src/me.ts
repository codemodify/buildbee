import { createContext, useContext } from "react";
import type { Person } from "./types";

// MeContext is the Person using this browser; every screen knows who is
// acting without guessing from member lists.
export const MeContext = createContext<Person | null>(null);

export function useMe(): Person | null {
  return useContext(MeContext);
}
