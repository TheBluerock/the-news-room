declare global {
  namespace App {
    interface Locals {
      user: { id: string; market: string; role: string } | null
    }
    interface PageData {
      user?: { id: string; market: string; role: string } | null
    }
  }
}
export {}
