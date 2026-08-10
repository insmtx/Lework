; 保留默认「正在运行，点击确定关闭」提示。
; 确认后按映像名 taskkill /F /T 结束进程树，再用 tasklist 校验一次；
; 避免默认逻辑按 $INSTDIR 路径匹配导致误判「仍在运行」而落入手动关闭兜底。

!macro customCheckAppRunning
	nsExec::Exec `"$SYSDIR\cmd.exe" /C tasklist /FI "IMAGENAME eq ${APP_EXECUTABLE_FILENAME}" /FO CSV /NH | "$SYSDIR\findstr.exe" /I /C:"${APP_EXECUTABLE_FILENAME}"`
	Pop $R0
	${if} $R0 == 0
		${if} ${isUpdated}
			Sleep 1000
		${else}
			MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION "$(appRunning)" /SD IDOK IDOK +2
			Quit
		${endIf}

		DetailPrint "$(appClosing)"
		nsExec::ExecToLog `"$SYSDIR\cmd.exe" /C taskkill /F /IM "${APP_EXECUTABLE_FILENAME}" /T`
		Pop $R1
		Sleep 500

		nsExec::Exec `"$SYSDIR\cmd.exe" /C tasklist /FI "IMAGENAME eq ${APP_EXECUTABLE_FILENAME}" /FO CSV /NH | "$SYSDIR\findstr.exe" /I /C:"${APP_EXECUTABLE_FILENAME}"`
		Pop $R0
		${if} $R0 == 0
			; 进程树尚未退净时再补一刀，仍在才走兜底提示。
			nsExec::ExecToLog `"$SYSDIR\cmd.exe" /C taskkill /F /IM "${APP_EXECUTABLE_FILENAME}" /T`
			Pop $R1
			Sleep 500
			nsExec::Exec `"$SYSDIR\cmd.exe" /C tasklist /FI "IMAGENAME eq ${APP_EXECUTABLE_FILENAME}" /FO CSV /NH | "$SYSDIR\findstr.exe" /I /C:"${APP_EXECUTABLE_FILENAME}"`
			Pop $R0
			${if} $R0 == 0
				MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(appCannotBeClosed)" /SD IDCANCEL IDRETRY +2
				Quit
				nsExec::ExecToLog `"$SYSDIR\cmd.exe" /C taskkill /F /IM "${APP_EXECUTABLE_FILENAME}" /T`
				Pop $R1
				Sleep 500
			${endIf}
		${endIf}
	${endIf}
!macroend
