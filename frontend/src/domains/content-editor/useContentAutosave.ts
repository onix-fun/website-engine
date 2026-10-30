import {
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { api, ApiError } from "@/api";
import type { EntryDetail, JsonObject } from "@/types";
import type { SaveState } from "./types";

interface AutosaveOptions {
  featureId: string;
  entryId: string | null;
  revision: number;
  data: JsonObject;
  enabled: boolean;
  createEntry?: (featureId: string, data: JsonObject) => Promise<EntryDetail>;
  onCreated: (entry: EntryDetail) => void;
  onSaved: (entry: EntryDetail) => void;
}

interface AutosaveResult {
  status: SaveState;
  error: unknown;
  conflict: string | null;
  lastSavedAt: Date | null;
  baseline: string;
  dirty: boolean;
  entryId: string | null;
  revision: number;
  save: (manual?: boolean) => Promise<boolean>;
  flush: () => Promise<boolean>;
  markLoaded: (baseline: string, entryId?: string | null, revision?: number) => void;
  markPublished: (entryId: string, revision: number) => void;
  adoptServerRevision: (revision: number) => void;
  clearConflict: () => void;
  setBaseline: (baseline: string) => void;
  getEntryId: () => string | null;
  getRevision: () => number;
}

const AUTOSAVE_DELAY = 900;

export function useContentAutosave({
  featureId,
  entryId,
  revision,
  data,
  enabled,
  createEntry = api.entries.create,
  onCreated,
  onSaved,
}: AutosaveOptions): AutosaveResult {
  const [status, setStatus] = useState<SaveState>("idle");
  const [error, setError] = useState<unknown>(null);
  const [conflict, setConflict] = useState<string | null>(null);
  const [lastSavedAt, setLastSavedAt] = useState<Date | null>(null);
  const [baseline, setBaselineState] = useState<string>(() =>
    JSON.stringify(data),
  );
  const [currentEntryId, setCurrentEntryId] = useState<string | null>(entryId);
  const [currentRevision, setCurrentRevision] = useState(revision);

  const dataRef = useRef(data);
  const dataJson = JSON.stringify(data);
  const dataJsonRef = useRef(dataJson);
  const baselineRef = useRef(baseline);
  const entryIdRef = useRef(entryId);
  const revisionRef = useRef(revision);
  const enabledRef = useRef(enabled);
  const statusRef = useRef(status);
  const savingRef = useRef<Promise<boolean> | null>(null);
  const createdCallbackRef = useRef(onCreated);
  const savedCallbackRef = useRef(onSaved);
  const entryIdPropRef = useRef(entryId);

  dataRef.current = data;
  dataJsonRef.current = dataJson;
  baselineRef.current = baseline;
  enabledRef.current = enabled;
  statusRef.current = status;
  createdCallbackRef.current = onCreated;
  savedCallbackRef.current = onSaved;

  // The hook owns the entry id and the revision once an entry exists. The
  // external id prop is only adopted when it actually changes, otherwise a
  // re-render with a stale prop would discard the id of a freshly created
  // entry and create a duplicate on the next autosave. The revision is only
  // taken from the prop when it is newer than the locally known one — the
  // authoritative value always arrives through markLoaded/markPublished.
  if (entryIdPropRef.current !== entryId) {
    entryIdPropRef.current = entryId;
    const switchingEntry = entryIdRef.current !== entryId;
    entryIdRef.current = entryId;
    setCurrentEntryId(entryId);
    if (switchingEntry && entryId) {
      revisionRef.current = revision;
      setCurrentRevision(revision);
    }
  }

  const dirty = enabled && dataJson !== baseline;

  const setBaseline = useCallback((next: string) => {
    baselineRef.current = next;
    setBaselineState(next);
  }, []);

  const markLoaded = useCallback(
    (nextBaseline: string, nextEntryId?: string | null, nextRevision?: number) => {
      setBaseline(nextBaseline);
      if (nextEntryId !== undefined) {
        entryIdRef.current = nextEntryId;
        setCurrentEntryId(nextEntryId);
      }
      if (nextRevision !== undefined) {
        revisionRef.current = nextRevision;
        setCurrentRevision(nextRevision);
      }
      setStatus("idle");
      setError(null);
      setConflict(null);
    },
    [setBaseline],
  );

  const markPublished = useCallback(
    (nextEntryId: string, nextRevision: number) => {
      // The baseline is already correct: the draft was flushed right before
      // the publish request, so the saved payload matches the local document.
      entryIdRef.current = nextEntryId;
      revisionRef.current = nextRevision;
      setCurrentEntryId(nextEntryId);
      setCurrentRevision(nextRevision);
      setStatus("saved");
      setLastSavedAt(new Date());
      setError(null);
      setConflict(null);
    },
    [],
  );

  const adoptServerRevision = useCallback((nextRevision: number) => {
    revisionRef.current = nextRevision;
    setCurrentRevision(nextRevision);
  }, []);

  const clearConflict = useCallback(() => {
    setConflict(null);
    setStatus((current) => (current === "conflict" ? "idle" : current));
  }, []);

  const performSave = useCallback(async (): Promise<boolean> => {
    if (!enabledRef.current) return false;
    if (savingRef.current) {
      await savingRef.current.catch(() => undefined);
    }
    const payload = dataRef.current;
    const payloadJson = JSON.stringify(payload);
    const currentEntryId = entryIdRef.current;
    if (currentEntryId && payloadJson === baselineRef.current) {
      setStatus("saved");
      return true;
    }

    setError(null);
    setConflict(null);
    setStatus("saving");
    statusRef.current = "saving";

    const task = (async (): Promise<boolean> => {
      try {
        let entry: EntryDetail;
        if (!currentEntryId) {
          entry = await createEntry(featureId, payload);
          entryIdRef.current = entry.id;
          revisionRef.current = entry.revision;
          setCurrentEntryId(entry.id);
          setCurrentRevision(entry.revision);
          createdCallbackRef.current(entry);
        } else {
          entry = await api.entries.saveDraft(
            currentEntryId,
            revisionRef.current,
            payload,
          );
          revisionRef.current = entry.revision;
          setCurrentRevision(entry.revision);
          savedCallbackRef.current(entry);
        }
        setBaseline(payloadJson);
        setLastSavedAt(new Date());
        setStatus("saved");
        statusRef.current = "saved";
        return true;
      } catch (saveError) {
        if (saveError instanceof ApiError && saveError.status === 409) {
          setConflict(saveError.message);
          setStatus("conflict");
          statusRef.current = "conflict";
        } else {
          setError(saveError);
          setStatus("error");
          statusRef.current = "error";
        }
        return false;
      }
    })();

    savingRef.current = task;
    try {
      return await task;
    } finally {
      savingRef.current = null;
    }
  }, [createEntry, featureId, setBaseline]);

  const save = useCallback(
    async (manual = false): Promise<boolean> => {
      if (!enabledRef.current) return false;
      const alreadyClean =
        Boolean(entryIdRef.current) && dataJsonRef.current === baselineRef.current;
      if (alreadyClean && !manual) {
        setStatus("saved");
        return true;
      }
      return performSave();
    },
    [performSave],
  );

  const flush = useCallback(async (): Promise<boolean> => {
    if (!enabledRef.current) return false;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      if (savingRef.current) {
        await savingRef.current.catch(() => undefined);
      }
      if (statusRef.current === "conflict" || statusRef.current === "error") {
        return false;
      }
      if (entryIdRef.current && dataJsonRef.current === baselineRef.current) {
        return true;
      }
      const ok = await performSave();
      if (!ok) return false;
    }
    return (
      Boolean(entryIdRef.current) &&
      dataJsonRef.current === baselineRef.current
    );
  }, [performSave]);

  // Autosave after a typing pause. It never runs in conflict/error state until
  // the user explicitly retries; local state always stays intact.
  useEffect(() => {
    if (!enabled || !dirty) return;
    if (status === "saving" || status === "error" || status === "conflict") return;
    const timer = window.setTimeout(() => {
      void performSave();
    }, AUTOSAVE_DELAY);
    return () => window.clearTimeout(timer);
  }, [dataJson, dirty, enabled, performSave, status]);

  return {
    status,
    error,
    conflict,
    lastSavedAt,
    baseline,
    dirty,
    entryId: currentEntryId,
    revision: currentRevision,
    save,
    flush,
    markLoaded,
    markPublished,
    adoptServerRevision,
    clearConflict,
    setBaseline,
    getEntryId: () => entryIdRef.current,
    getRevision: () => revisionRef.current,
  };
}
