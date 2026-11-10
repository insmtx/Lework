"use client";

import { Button } from "@lework/ui/components/ui/button";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { Loader2, RotateCcw, TerminalSquare } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { closePTY, createPTY } from "../lib/api";

type TerminalStatus = "creating" | "connecting" | "ready" | "disconnected" | "exited" | "error";

interface TerminalPanelProps {
	nodeID: string;
	sessionID: string;
	directory: string;
}

export function TerminalPanel({ nodeID, sessionID, directory }: TerminalPanelProps) {
	const containerRef = useRef<HTMLDivElement>(null);
	const [status, setStatus] = useState<TerminalStatus>("creating");
	const [error, setError] = useState<string | null>(null);
	const [attempt, setAttempt] = useState(0);

	// biome-ignore lint/correctness/useExhaustiveDependencies: attempt is the reconnect trigger
	useEffect(() => {
		const container = containerRef.current;
		if (!container) return;

		let disposed = false;
		let socket: WebSocket | null = null;
		let ptyID: string | null = null;
		let exited = false;
		let observer: ResizeObserver | null = null;

		container.innerHTML = "";
		setStatus("creating");
		setError(null);

		const terminal = new Terminal({
			cursorBlink: true,
			fontSize: 13,
			fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
			scrollback: 5000,
			theme: {
				background: "#0b1120",
				foreground: "#dbeafe",
				cursor: "#93c5fd",
				selectionBackground: "#1d4ed855",
			},
		});
		const fit = new FitAddon();
		terminal.loadAddon(fit);
		terminal.open(container);

		const fitTerminal = () => {
			try {
				fit.fit();
			} catch {
				// xterm throws while the container is hidden; the next resize fixes it.
			}
		};
		fitTerminal();

		const sendResize = () => {
			fitTerminal();
			if (socket?.readyState === WebSocket.OPEN) {
				socket.send(JSON.stringify({ type: "resize", cols: terminal.cols, rows: terminal.rows }));
			}
		};

		terminal.onData((data) => {
			if (socket?.readyState === WebSocket.OPEN) {
				socket.send(new TextEncoder().encode(data));
			}
		});

		observer = new ResizeObserver(() => {
			requestAnimationFrame(sendResize);
		});
		observer.observe(container);

		const run = async () => {
			try {
				const pty = await createPTY(
					nodeID,
					sessionID,
					directory,
					terminal.cols || 80,
					terminal.rows || 24,
				);
				ptyID = pty.id;
				if (disposed) {
					void closePTY(nodeID, sessionID, pty.id);
					return;
				}
				setStatus("connecting");
				const scheme = window.location.protocol === "https:" ? "wss" : "ws";
				const url = `${scheme}://${window.location.host}/api/pty/${encodeURIComponent(nodeID)}/${encodeURIComponent(sessionID)}/${encodeURIComponent(pty.id)}/ws`;
				socket = new WebSocket(url);
				socket.binaryType = "arraybuffer";
				socket.onopen = () => {
					if (disposed) {
						socket?.close();
						return;
					}
					setStatus("ready");
					sendResize();
					terminal.focus();
				};
				socket.onmessage = (message) => {
					if (typeof message.data === "string") {
						try {
							const control = JSON.parse(message.data) as { type?: string; message?: string };
							if (control.type === "exit") {
								exited = true;
								setStatus("exited");
								terminal.write("\r\n\x1b[2m[process exited]\x1b[0m\r\n");
							} else if (control.type === "error") {
								setError(control.message || "Terminal error");
								setStatus("error");
							}
						} catch {
							// Ignore unknown control frames.
						}
						return;
					}
					terminal.write(new Uint8Array(message.data as ArrayBuffer));
				};
				socket.onerror = () => {
					if (!disposed) setError("Terminal WebSocket error");
				};
				socket.onclose = () => {
					if (!disposed && !exited) setStatus("disconnected");
				};
			} catch (caught) {
				if (disposed) return;
				setStatus("error");
				setError(caught instanceof Error ? caught.message : "Unable to start terminal");
			}
		};

		void run();

		return () => {
			disposed = true;
			observer?.disconnect();
			socket?.close();
			terminal.dispose();
			if (ptyID) void closePTY(nodeID, sessionID, ptyID);
		};
	}, [nodeID, sessionID, directory, attempt]);

	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div className="flex items-center gap-2 border-b px-4 py-2">
				<TerminalSquare className="size-4 text-muted-foreground" />
				<span className="text-xs font-medium">Session terminal</span>
				<span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
					{status}
				</span>
				{error ? <span className="truncate text-[11px] text-destructive">{error}</span> : null}
				<Button
					variant="ghost"
					size="sm"
					className="ml-auto"
					onClick={() => setAttempt((value) => value + 1)}
					disabled={status === "creating" || status === "connecting"}
				>
					<RotateCcw className="size-3.5" /> Reconnect
				</Button>
			</div>
			<div className="relative min-h-0 flex-1 bg-[#0b1120]">
				<div ref={containerRef} className="h-full w-full p-2" />
				{status === "creating" || status === "connecting" ? (
					<div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-[#0b1120]/80 text-xs text-slate-300">
						<Loader2 className="mr-2 size-4 animate-spin" /> Starting terminal
					</div>
				) : null}
				{status === "error" && !error ? (
					<div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-destructive">
						Terminal failed to start
					</div>
				) : null}
			</div>
		</div>
	);
}
