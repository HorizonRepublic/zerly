import { Injectable, Logger, VERSION_NEUTRAL } from '@nestjs/common';
import { PATH_METADATA, VERSION_METADATA } from '@nestjs/common/constants';
import { DiscoveryService, MetadataScanner, Reflector } from '@nestjs/core';

// Internal constants from @nestjs/microservices to avoid hard dependency
const PATTERN_METADATA = 'microservices:pattern';
const TRANSPORT_METADATA = 'microservices:transport';
const PATTERN_HANDLER_METADATA = 'microservices:handler_type'; // 1 = Event, 0 = Message

type RouteTree = Record<string, Record<string, string[]>>;
type VersionMeta = string | string[] | typeof VERSION_NEUTRAL | undefined;

// Supported route kinds
type RouteKind = 'HTTP' | 'RPC';

interface IRouteDefinition {
  kind: RouteKind;
  method: string; // HTTP Verb (GET, POST) or RPC Type (CMD, EVT)
  pathOrPattern: string; // URL path or JSON pattern
  version?: VersionMeta; // Version is usually relevant for HTTP
}

const ANSI = {
  reset: '\x1b[0m',
  gray: '\x1b[90m',
  magenta: '\x1b[35m',
  cyan: '\x1b[36m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  blue: '\x1b[34m',
  red: '\x1b[31m',
} as const;

/**
 * Service responsible for inspecting and logging all registered routes (HTTP and RPC).
 *
 * It provides a hierarchical view:
 * Module -> Controller -> [HTTP Routes & RPC Handlers]
 */
@Injectable()
export class RoutesInspectorProvider {
  private readonly logger = new Logger('RoutesInspector');

  public constructor(
    private readonly discoveryService: DiscoveryService,
    private readonly metadataScanner: MetadataScanner,
    private readonly reflector: Reflector,
  ) {}

  /**
   * Scans the application for registered controllers and prints the route tree.
   */
  public inspect(): void {
    const tree = this.buildRouteTree();

    if (this.isEmptyTree(tree)) {
      return;
    }

    this.printTree(tree);
  }

  // ---------------------------------------------------------------------------
  // Tree building
  // ---------------------------------------------------------------------------

  private buildRouteTree(): RouteTree {
    const controllers = this.discoveryService.getControllers();
    const tree: RouteTree = {};

    for (const wrapper of controllers) {
      const context = this.getControllerContext(wrapper);

      if (!context) continue;

      const routes = this.collectControllerRoutes(context);

      if (routes.length === 0) continue;

      this.putRoutes(tree, context.moduleName, context.controllerName, routes);
    }

    return tree;
  }

  private putRoutes(
    tree: RouteTree,
    moduleName: string,
    controllerName: string,
    routes: string[],
  ): void {
    tree[moduleName] ??= {};
    tree[moduleName][controllerName] = routes;
  }

  private isEmptyTree(tree: RouteTree): boolean {
    return Object.keys(tree).length === 0;
  }

  // ---------------------------------------------------------------------------
  // Metadata Extraction
  // ---------------------------------------------------------------------------

  /**
   * Extracts context information about a controller wrapper.
   */
  private getControllerContext(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    wrapper: any,
  ): {
    instance: object;
    controllerName: string;
    moduleName: string;
    basePath: string;
    controllerVersion: VersionMeta;
  } | null {
    const instance = wrapper?.instance as object | undefined;
    const controllerName = wrapper?.name as string | undefined;

    if (!instance || !controllerName) return null;

    const moduleName = wrapper?.host?.name ?? 'UnknownModule';

    // HTTP specific: Controller-level path
    const basePath = this.normalizePath(
      this.pickFirst(this.reflector.get<string | string[]>(PATH_METADATA, instance.constructor)) ??
        '',
    );

    // HTTP specific: Controller-level version
    const controllerVersion = this.reflector.get<VersionMeta>(
      VERSION_METADATA,
      instance.constructor,
    );

    return { instance, controllerName, moduleName, basePath, controllerVersion };
  }

  /**
   * Iterates over all methods of a controller to find HTTP and RPC handlers.
   */
  private collectControllerRoutes(context: {
    instance: object;
    basePath: string;
    controllerVersion: VersionMeta;
  }): string[] {
    const prototype = Object.getPrototypeOf(context.instance);
    const methodNames = this.metadataScanner.getAllMethodNames(prototype);

    const renderedRoutes: string[] = [];

    for (const methodName of methodNames) {
      const methodRef = (context.instance as Record<string, unknown>)[methodName];

      // 1. Try to extract HTTP definition
      const httpRoute = this.extractHttpDefinition(methodRef, context);

      if (httpRoute) {
        renderedRoutes.push(this.formatRoute(httpRoute));
      }

      // 2. Try to extract RPC definition (Microservices)
      const rpcRoute = this.extractRpcDefinition(methodRef);

      if (rpcRoute) {
        renderedRoutes.push(this.formatRoute(rpcRoute));
      }
    }

    return renderedRoutes;
  }

  // --- HTTP Extraction ---

  private extractHttpDefinition(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    methodRef: any,
    context: { basePath: string; controllerVersion: VersionMeta },
  ): IRouteDefinition | null {
    const pathMeta = this.reflector.get<string | string[] | undefined>(PATH_METADATA, methodRef);
    const methodId = this.reflector.get<number | undefined>('method', methodRef);

    if (typeof pathMeta === 'undefined' || typeof methodId === 'undefined') {
      return null;
    }

    const methodVersion = this.reflector.get<VersionMeta>(VERSION_METADATA, methodRef);
    const effectiveVersion = methodVersion ?? context.controllerVersion;

    const path = this.normalizePath(this.pickFirst(pathMeta) ?? '');
    const fullPath = this.buildFullPath(context.basePath, path);

    return {
      kind: 'HTTP',
      method: this.mapHttpMethodToString(methodId),
      pathOrPattern: fullPath,
      version: effectiveVersion,
    };
  }

  // --- RPC Extraction ---

  private extractRpcDefinition(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    methodRef: any,
  ): IRouteDefinition | null {
    const pattern = this.reflector.get<object | string | undefined>(PATTERN_METADATA, methodRef);

    if (typeof pattern === 'undefined') {
      return null;
    }

    const handlerType = this.reflector.get<number | undefined>(PATTERN_HANDLER_METADATA, methodRef);
    const transport = this.reflector.get<number | undefined>(TRANSPORT_METADATA, methodRef);

    // 1 = EventPattern, 0 (or undefined) = MessagePattern
    const isEvent = handlerType === 2; // NestJS internals: 2 is Event, 1 is RequestResponse usually, but check depends on version.
    // Actually simpler: typically we just distinguish "Event" vs "CMD" for visualization.
    // Let's assume generic RPC if unsure.

    const methodLabel = isEvent ? 'EVT' : 'RPC';
    const transportLabel = transport !== undefined ? `[T:${transport}]` : ''; // Optional transport info

    return {
      kind: 'RPC',
      method: `${methodLabel}${transportLabel}`, // e.g., "RPC" or "RPC[T:1]"
      pathOrPattern: JSON.stringify(pattern),
      version: undefined, // RPC usually doesn't use @Version
    };
  }

  // ---------------------------------------------------------------------------
  // Output Formatting
  // ---------------------------------------------------------------------------

  private formatRoute(def: IRouteDefinition): string {
    const methodColored = this.colorizeMethod(def.method, def.kind);
    const versionStr = def.version ? this.formatVersion(def.version) : '';

    return `${versionStr}${methodColored} ${def.pathOrPattern}`;
  }

  private printTree(tree: RouteTree): void {
    this.logger.log('Mapped Routes:');

    for (const [moduleName, controllers] of Object.entries(tree)) {
      this.logger.log(`${ANSI.magenta}[Module] ${moduleName}${ANSI.reset}`);

      for (const [controllerName, routes] of Object.entries(controllers)) {
        this.logger.log(`  ${ANSI.cyan}${controllerName}${ANSI.reset}`);

        routes.forEach((route, index) => {
          const prefix = index === routes.length - 1 ? '└──' : '├──';

          this.logger.log(`   ${prefix} ${route}`);
        });
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  private buildFullPath(basePath: string, routePath: string): string {
    const joined = `/${basePath}/${routePath}`.replace(/\/+/g, '/').replace(/\/$/, '');

    return joined.length === 0 ? '/' : joined;
  }

  private normalizePath(path: string | undefined): string {
    if (!path) return '';
    return path.startsWith('/') ? path.slice(1) : path;
  }

  private pickFirst<T>(value: T | T[] | undefined): T | undefined {
    return Array.isArray(value) ? value[0] : value;
  }

  private formatVersion(version: VersionMeta): string {
    if (!version) return '';
    if (version === VERSION_NEUTRAL) return `${ANSI.gray}[Neutral]${ANSI.reset} `;
    const content = Array.isArray(version) ? version.join(',') : String(version);

    return `${ANSI.gray}[v${content}]${ANSI.reset} `;
  }

  private mapHttpMethodToString(id: number): string {
    switch (id) {
      case 0:
        return 'GET';
      case 1:
        return 'POST';
      case 2:
        return 'PUT';
      case 3:
        return 'DELETE';
      case 4:
        return 'PATCH';
      case 5:
        return 'ALL';
      case 6:
        return 'OPTIONS';
      case 7:
        return 'HEAD';
      default:
        return 'ANY';
    }
  }

  private colorizeMethod(method: string, kind: RouteKind): string {
    // Special handling for RPC
    if (kind === 'RPC') {
      return `${ANSI.blue}${method}${ANSI.reset}`;
    }

    // Standard HTTP coloring
    switch (method) {
      case 'GET':
        return `${ANSI.green}${method}${ANSI.reset}`;
      case 'POST':
        return `${ANSI.yellow}${method}${ANSI.reset}`;
      case 'PUT':
        return `${ANSI.blue}${method}${ANSI.reset}`;
      case 'DELETE':
        return `${ANSI.red}${method}${ANSI.reset}`;
      case 'PATCH':
        return `${ANSI.magenta}${method}${ANSI.reset}`;
      default:
        return `${ANSI.reset}${method}${ANSI.reset}`;
    }
  }
}
