import React from "react";
import { createRoot } from "react-dom/client";
import * as JSXRuntime from "react/jsx-runtime";
import { RuntimeApp } from "./RuntimeApp";
import "./runtime.css";

declare global {
  interface Window {
    __VITASIL_RUNTIME__?: {
      React: typeof React;
      JSXRuntime: typeof JSXRuntime;
    };
  }
}

// Feature ESM archives externalize React and resolve it through this stable
// runtime ABI, so hooks and elements share the renderer's React instance.
window.__VITASIL_RUNTIME__ = { React, JSXRuntime };

const root = document.getElementById("root");
if (!root) throw new Error("Public runtime root element is missing");
createRoot(root).render(<RuntimeApp />);
