"use client";

import { useCallback, useRef, useState } from "react";
import { authenticatedFetch, API_BASE_URL, formatFileSize, skillMarketplaceApi } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Input } from "@leros/ui/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import { cn } from "@leros/ui/lib/utils";
import { FileArchive, FileText, GitBranch, Loader2, Upload, X } from "lucide-react";
import { toast } from "sonner";

export type SkillImportDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const ALLOWED_EXTENSIONS = [".zip", ".md"];
const TAB_PANEL_H = "h-[6rem]";
const TAB_PANEL_GRID =
	"grid [&>[data-slot=tabs-content]]:col-start-1 [&>[data-slot=tabs-content]]:row-start-1";
type ImportMode = "file" | "github";

function getFileExtension(filename: string): string {
  const idx = filename.lastIndexOf(".");
  if (idx === -1) return "";
  return filename.slice(idx).toLowerCase();
}

function isValidFile(file: File): boolean {
  const ext = getFileExtension(file.name);
  return ALLOWED_EXTENSIONS.includes(ext);
}

function isLikelyGitHubSkillURL(value: string): boolean {
  const input = value.trim();
  if (!input) return false;

  if (!input.includes("://")) {
    return /^[^/\s]+\/[^/\s]+\/.+/.test(input);
  }

  try {
    const parsed = new URL(input);
    return ["github.com", "www.github.com", "raw.githubusercontent.com"].includes(
        parsed.hostname.toLowerCase(),
    );
  } catch {
    return false;
  }
}

export function SkillImportDialog({ open, onOpenChange }: SkillImportDialogProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [importMode, setImportMode] = useState<ImportMode>("file");
  const [file, setFile] = useState<File | null>(null);
  const [githubUrl, setGithubUrl] = useState("");
  const [status, setStatus] = useState<"idle" | "selected" | "uploading" | "error">("idle");
  const [errorMessage, setErrorMessage] = useState("");
  const [dragActive, setDragActive] = useState(false);

  const reset = useCallback(() => {
    setImportMode("file");
    setFile(null);
    setGithubUrl("");
    setStatus("idle");
    setErrorMessage("");
    setDragActive(false);
    if (inputRef.current) {
      inputRef.current.value = "";
    }
  }, []);

  const handleClose = useCallback(() => {
    reset();
    onOpenChange(false);
  }, [onOpenChange, reset]);

  const validateAndSetFile = useCallback((f: File) => {
    if (!isValidFile(f)) {
      setFile(null);
      setStatus("error");
      setErrorMessage("仅支持 .zip 和 .md 格式的文件");
      return;
    }
    setFile(f);
    setStatus("selected");
    setErrorMessage("");
  }, []);

  // ---- click to select ----
  const handleDropZoneClick = useCallback(() => {
    inputRef.current?.click();
  }, []);

  const handleInputChange = useCallback(
      (e: React.ChangeEvent<HTMLInputElement>) => {
        const f = e.target.files?.[0];
        if (!f) return;
        validateAndSetFile(f);
      },
      [validateAndSetFile],
  );

  // ---- drag and drop ----
  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(false);
  }, []);

  const handleDrop = useCallback(
      (e: React.DragEvent) => {
        e.preventDefault();
        e.stopPropagation();
        setDragActive(false);

        const f = e.dataTransfer.files?.[0];
        if (!f) return;
        validateAndSetFile(f);
      },
      [validateAndSetFile],
  );

  // ---- remove selected file ----
  const handleRemoveFile = useCallback(() => {
    setFile(null);
    setStatus("idle");
    setErrorMessage("");
    if (inputRef.current) {
      inputRef.current.value = "";
    }
  }, []);

  const handleModeChange = useCallback((value: string) => {
    setImportMode(value as ImportMode);
    setStatus((current) => (current === "uploading" ? current : "idle"));
    setErrorMessage("");
    setDragActive(false);
  }, []);

  // ---- upload + import ----
  const handleImport = useCallback(async () => {
    if (importMode === "github") {
      const trimmedUrl = githubUrl.trim();
      if (!trimmedUrl) {
        setStatus("error");
        setErrorMessage("请输入 GitHub 链接或 owner/repo/path");
        return;
      }
      if (!isLikelyGitHubSkillURL(trimmedUrl)) {
        setStatus("error");
        setErrorMessage("请输入 GitHub tree/blob/raw 链接，或 owner/repo/path");
        return;
      }

      setStatus("uploading");
      setErrorMessage("");

      try {
        await skillMarketplaceApi.importFromGitHub({
          github_url: trimmedUrl,
        });

        toast.success("技能导入请求已提交");
        handleClose();
      } catch (err: any) {
        setStatus("error");
        setErrorMessage(err?.message ?? "导入失败，请重试");
      }
      return;
    }

    if (!file) return;

    setStatus("uploading");
    setErrorMessage("");

    try {
      // Step 1: Upload file
      const formData = new FormData();
      formData.append("file", file);
      formData.append("purpose", "project");

      const uploadResponse = await authenticatedFetch(`${API_BASE_URL}/files/upload`, {
        method: "POST",
        body: formData,
      });

      if (!uploadResponse.ok) {
        let msg = `HTTP ${uploadResponse.status}`;
        try {
          const payload = (await uploadResponse.json()) as { message?: string };
          if (typeof payload.message === "string" && payload.message) {
            msg = payload.message;
          }
        } catch {
          // keep default error message
        }
        throw new Error(msg);
      }

      // Step 2: Extract file_upload_id from response
      const uploadData = (await uploadResponse.json()) as {
        data?: { file_upload_id?: string };
      };
      const fileUploadId = uploadData?.data?.file_upload_id;
      if (!fileUploadId) {
        throw new Error("上传接口未返回 file_upload_id");
      }

      // Step 3: Call import API via store (HttpClient throws ApiError on failure)
      await skillMarketplaceApi.import({
        file_upload_id: fileUploadId,
      });

      toast.success("技能导入请求已提交");
      handleClose();
    } catch (err: any) {
      setStatus("error");
      setErrorMessage(err?.message ?? "导入失败，请重试");
    }
  }, [file, githubUrl, handleClose, importMode]);

  // ---- file type icon ----
  const FileIcon = file?.name?.endsWith(".zip") ? FileArchive : FileText;
  const importDisabled =
      status === "uploading" ||
      (importMode === "file" && !file) ||
      (importMode === "github" && !githubUrl.trim());

  return (
      <Dialog open={open} onOpenChange={handleClose}>
        <DialogContent
            className="sm:max-w-md border-[var(--leros-control-border)] bg-white text-[var(--leros-text-strong)] shadow-[var(--leros-shadow-menu)]"
            showCloseButton={false}
        >
          <DialogHeader>
            <DialogTitle>导入技能</DialogTitle>
            <DialogDescription className="text-[var(--leros-text-muted)]">
              上传 .zip / .md 文件，或粘贴 GitHub 链接导入
            </DialogDescription>
          </DialogHeader>

          <Tabs value={importMode} onValueChange={handleModeChange} className="mt-2">
            <TabsList className="w-full bg-[var(--leros-surface-soft)]">
              <TabsTrigger
                  value="file"
                  className="w-full text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)] dark:hover:text-[var(--leros-text-strong)] data-active:bg-white data-active:text-[var(--leros-text-strong)] dark:data-active:bg-white dark:data-active:text-[var(--leros-text-strong)]"
              >
                <Upload className="size-4" />
                上传文件
              </TabsTrigger>
              <TabsTrigger
                  value="github"
                  className="w-full text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)] dark:hover:text-[var(--leros-text-strong)] data-active:bg-white data-active:text-[var(--leros-text-strong)] dark:data-active:bg-white dark:data-active:text-[var(--leros-text-strong)]"
              >
                <GitBranch className="size-4" />
                GitHub 链接
              </TabsTrigger>
            </TabsList>

            <div className={cn("relative mt-3", TAB_PANEL_H, TAB_PANEL_GRID)}>
            <TabsContent value="file" className="mt-0 h-full min-h-0">
              {/* ---- drop zone ---- */}
              <div
                  role="button"
                  tabIndex={0}
                  onClick={handleDropZoneClick}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") handleDropZoneClick();
                  }}
                  onDragOver={handleDragOver}
                  onDragLeave={handleDragLeave}
                  onDrop={handleDrop}
                  className={cn(
                      "border-2 border-dashed rounded-lg px-4 py-3 text-center cursor-pointer transition-colors h-full flex flex-col justify-center",
                      "border-[var(--leros-control-border)]",
                      "hover:border-[var(--leros-text-muted)]",
                      dragActive && "border-primary bg-primary/5",
                      status === "selected" && "border-[var(--leros-text-muted)]",
                  )}
              >
                <input
                    ref={inputRef}
                    type="file"
                    accept=".zip,.md"
                    className="hidden"
                    onChange={handleInputChange}
                />

                {status === "idle" || status === "error" ? (
                    <div className="mt-1 flex flex-col items-center gap-1.5">
                      <Upload className="size-8 text-[var(--leros-text-muted)]" />
                      <p className="text-sm text-[var(--leros-text-strong)] text-center">
                        拖拽文件到此处，或点击选择文件
                      </p>
                      <p className="text-xs text-[var(--leros-text-muted)] text-center">
                        支持 .zip 和 .md 格式
                      </p>
                    </div>
                ) : (
                    <div className="flex items-center gap-3 px-2">
                      <FileIcon className="size-8 shrink-0 text-[var(--leros-text-muted)]" />
                      <div className="flex-1 min-w-0 text-left">
                        <p className="text-sm font-medium text-[var(--leros-text-strong)] truncate">
                          {file?.name}
                        </p>
                        <p className="text-xs text-[var(--leros-text-muted)]">
                          {file ? formatFileSize(file.size) : ""}
                        </p>
                      </div>
                      <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            handleRemoveFile();
                          }}
                          className="shrink-0 p-1 rounded hover:bg-[var(--leros-control-bg)] transition-colors"
                          aria-label="移除文件"
                      >
                        <X className="size-4 text-[var(--leros-text-muted)]" />
                      </button>
                    </div>
                )}
              </div>
            </TabsContent>

            <TabsContent value="github" className="mt-0 h-full min-h-0">
              <div className="flex h-full flex-col justify-center space-y-2">
                <div className="flex items-center gap-2">
                  <GitBranch className="size-4 shrink-0 text-[var(--leros-text-muted)]" />
                  <Input
                      value={githubUrl}
                      onChange={(e) => {
                        setGithubUrl(e.target.value);
                        if (status === "error") {
                          setStatus("idle");
                          setErrorMessage("");
                        }
                      }}
                      placeholder="github.com/owner/repo/tree/main/path/to/skill"
                      disabled={status === "uploading"}
                      aria-label="GitHub 技能链接"
                      className="border-[var(--leros-control-border)] bg-white text-[var(--leros-text-strong)] placeholder:text-[var(--leros-text-muted)] dark:bg-white"
                  />
                </div>
                <p className="text-xs text-[var(--leros-text-muted)]">
                  支持 tree、blob、raw 链接，或 owner/repo/skillPath
                </p>
              </div>
            </TabsContent>
            </div>
          </Tabs>

          {/* ---- error banner ---- */}
          {status === "error" && errorMessage && (
              <p className="text-sm text-red-600 mt-2">{errorMessage}</p>
          )}

          <DialogFooter className="mt-3">
            <Button variant="outline" onClick={handleClose}>
              取消
            </Button>
            <Button onClick={handleImport} disabled={importDisabled}>
              {status === "uploading" ? (
                  <>
                    <Loader2 className="size-4 mr-1 animate-spin" />
                    导入中...
                  </>
              ) : (
                  "导入"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
  );
}
