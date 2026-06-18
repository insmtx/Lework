"use client";

import { SkillDetailView } from "@leros/app-ui";
import { skillMarketplaceApi } from "@leros/store";
import { useParams, useRouter } from "next/navigation";
import { useCallback } from "react";
import { toast } from "sonner";

export default function SkillDetailPage() {
  const params = useParams<{ skillId: string }>();
  const router = useRouter();
  const skillId = params.skillId;

  const handleUse = useCallback(
    (_skillId: string) => {
      router.push("/");
    },
    [router],
  );

  const handleUninstall = useCallback(
    async (name: string) => {
      try {
        await skillMarketplaceApi.uninstall({ name });
        toast.success("卸载已提交");
        router.push("/skills");
      } catch (err: any) {
        const msg = err?.response?.data?.message ?? err?.message ?? "未知错误";
        toast.error(`卸载失败：${msg}`);
      }
    },
    [router],
  );

  return (
    <SkillDetailView
      skillId={skillId}
      onBack={() => router.push("/skills")}
      onSkillClick={(id) => router.push(`/skills/${id}`)}
      onUse={handleUse}
      onUninstall={handleUninstall}
    />
  );
}
