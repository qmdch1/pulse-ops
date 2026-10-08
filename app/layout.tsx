import type { Metadata } from "next";
import "./globals.css";
import "./workspace.css";
import "./assets.css";
import "./readability.css";
export const metadata: Metadata = {title:"PULSE / OPS — 통합 서버 모니터링",description:"이벤트, 위험 징후와 서버 지표를 하나의 화면에서 확인하는 운영 관제 대시보드.",icons:{icon:"/favicon.svg",shortcut:"/favicon.svg"}};
export default function RootLayout({children}:Readonly<{children:React.ReactNode}>){return <html lang="ko" className="dark"><body>{children}</body></html>}
