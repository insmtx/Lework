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
import { cn } from "@leros/ui/lib/utils";
import { FileArchive, FileText, Loader2, Upload, X } from "lucide-react";
import { toast } from "sonner";

export type SkillImportDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const ALLOWED_EXTENSIONS = [".zip", ".md"];

function getFileExtension(filename: string): string {
  const idx = filename.lastIndexOf(".");
  if (idx === -1) return "";
  return filename.slice(idx).toLowerCase();
}

function isValidFile(file: File): boolean {
  const ext = getFileExtension(file.name);
  return ALLOWED_EXTENSIONS.includes(ext);
}

export function SkillImportDialog({ open, onOpenChange }: SkillImportDialogProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [status, setStatus] = useState<"idle" | "selected" | "uploading" | "error">("idle");
  const [errorMessage, setErrorMessage] = useState("");
  const [dragActive, setDragActive] = useState(false);

  const reset = useCallback(() => {
    setFile(null);
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

  // ---- upload + import ----
  const handleImport = useCallback(async () => {
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
  }, [file, handleClose]);

  // ---- file type icon ----
  const FileIcon = file?.name?.endsWith(".zip") ? FileArchive : FileText;

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>导入技能</DialogTitle>
          <DialogDescription>上传 .zip 或 .md 技能文件以导入</DialogDescription>
        </DialogHeader>

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
            "mt-2 border-2 border-dashed rounded-lg p-8 text-center cursor-pointer transition-colors",
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
            <div className="flex flex-col items-center gap-2">
              <Upload className="size-10 text-[var(--leros-text-muted)]" />
              <p className="text-sm text-[var(--leros-text-strong)]">
                拖拽文件到此处，或点击选择文件
              </p>
              <p className="text-xs text-[var(--leros-text-muted)]">
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

        {/* ---- error banner ---- */}
        {status === "error" && errorMessage && (
          <p className="text-sm text-red-600 mt-2">{errorMessage}</p>
        )}

        <DialogFooter className="mt-4">
          <Button variant="outline" onClick={handleClose}>
            取消
          </Button>
          <Button onClick={handleImport} disabled={!file || status === "uploading"}>
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
