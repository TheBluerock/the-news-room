
// this file is generated — do not edit it


declare module "svelte/elements" {
	export interface HTMLAttributes<T> {
		'data-sveltekit-keepfocus'?: true | '' | 'off' | undefined | null;
		'data-sveltekit-noscroll'?: true | '' | 'off' | undefined | null;
		'data-sveltekit-preload-code'?:
			| true
			| ''
			| 'eager'
			| 'viewport'
			| 'hover'
			| 'tap'
			| 'off'
			| undefined
			| null;
		'data-sveltekit-preload-data'?: true | '' | 'hover' | 'tap' | 'off' | undefined | null;
		'data-sveltekit-reload'?: true | '' | 'off' | undefined | null;
		'data-sveltekit-replacestate'?: true | '' | 'off' | undefined | null;
	}
}

export {};


declare module "$app/types" {
	type MatcherParam<M> = M extends (param : string) => param is (infer U extends string) ? U : string;

	export interface AppTypes {
		RouteId(): "/" | "/analytics" | "/api" | "/api/health" | "/audit" | "/calendar" | "/corrections" | "/login" | "/logout" | "/moderation";
		RouteParams(): {
			
		};
		LayoutParams(): {
			"/": Record<string, never>;
			"/analytics": Record<string, never>;
			"/api": Record<string, never>;
			"/api/health": Record<string, never>;
			"/audit": Record<string, never>;
			"/calendar": Record<string, never>;
			"/corrections": Record<string, never>;
			"/login": Record<string, never>;
			"/logout": Record<string, never>;
			"/moderation": Record<string, never>
		};
		Pathname(): "/" | "/analytics" | "/api/health" | "/audit" | "/calendar" | "/corrections" | "/login" | "/logout" | "/moderation";
		ResolvedPathname(): `${"" | `/${string}`}${ReturnType<AppTypes['Pathname']>}`;
		Asset(): string & {};
	}
}